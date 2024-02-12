package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	nrpe "github.com/canonical/nrped/common"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/spacemonkeygo/openssl"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	listenAddress = kingpin.Flag("web.listen-address", "The address to listen on for HTTP requests.").Default(":9275").String()
)

// Command represents command name and argument list
type Command struct {
	Name string
	Args []string
}

// NewCommand creates Command object with the given name and optional argument list
func NewCommand(name string, args ...string) Command {
	return Command{
		Name: name,
		Args: args,
	}
}

// toStatusLine convers Command content to single status line string
func (c Command) toStatusLine() string {
	if c.Args != nil && len(c.Args) > 0 {
		args := strings.Join(c.Args, "!")
		return c.Name + "!" + args
	}

	return c.Name
}

func ParsePerfdata(perfdata string) map[string]string {
	// Parse perfdata
	// Example: "load1=0.000;15.000;30.000;0; load5=0.000;10.000;25.000;0; load15=0.000;5.000;20.000;0;"
	perfdata = strings.Trim(perfdata, " ")

	// Split perfdata into individual metrics and present them as key-value pairs
	metrics := strings.Split(perfdata, ";")
	mapPerfdata := make(map[string]string)

	for _, metric := range metrics {
		parts := strings.Split(metric, "=")
		if len(parts) < 2 {
			continue
		}
		key := strings.Trim(parts[0], " ")

		// Remove any units from the value
		parts[1] = strings.Replace(parts[1], "MB", "", -1)

		value := strings.Trim(parts[1], " ")
		mapPerfdata[key] = value
	}
	return mapPerfdata
}

// Collector type containing issued command and a logger
type Collector struct {
	command string
	target  string
	ssl     bool
	logger  log.Logger
	prefix  string
}

// CommandResult type describing the result of command against nrpe-server
type CommandResult struct {
	commandDuration float64
	statusOk        float64
	result          *nrpe.NrpePacket
	Perfdata        map[string]string
}

// Describe implemented with dummy data to satisfy interface
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- prometheus.NewDesc("dummy", "dummy", nil, nil)
}

func collectCommandMetrics(cmd string, conn net.Conn, logger log.Logger) (CommandResult, error) {
	// Parse and issue given command

	level.Info(logger).Log("msg", "Sending command", "command", cmd)
	nrpeCmd := NewCommand(cmd)
	nrpeCommand := nrpeCmd.toStatusLine()

	command := nrpe.PrepareToSend(nrpeCommand, nrpe.QUERY_PACKET)
	level.Info(logger).Log("msg", "Prepared command", "command", command)
	startTime := time.Now()

	err := nrpe.SendPacket(conn, command)
	if err != nil {
		return CommandResult{
			commandDuration: time.Since(startTime).Seconds(),
			statusOk:        0,
			result:          nil,
		}, err
	}

	result, err := nrpe.ReceivePacket(conn)
	logger.Log("msg", "Received packet", "result", result, "err", err)
	if err != nil {
		level.Error(logger).Log("msg", "ERROR!", err)
		return CommandResult{
			commandDuration: time.Since(startTime).Seconds(),
			statusOk:        0,
			result:          nil,
		}, err
	}

	duration := time.Since(startTime).Seconds()
	ipaddr, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	output := result.CommandBuffer[:]

	// Log the result of the command
	rawOutput := fmt.Sprintf("%s", bytes.Trim(output, "\x00"))
	level.Info(logger).Log("msg", "Command returned", "address", ipaddr, "duration", duration, "return_code", result.ResultCode, "command_output", rawOutput)

	tempOutput := strings.Split(rawOutput, "|")

	// perfdata is a map of key-value pairs representing the performance data
	// the key is the name of the metric and the value is the value of the metric
	var perfdata map[string]string
	if len(tempOutput) >= 1 {
		fmt.Println("potential perfdata ", tempOutput[1])
		pre := strings.TrimRight(tempOutput[1], " ")
		pre = strings.TrimLeft(pre, " ")
		perfdata = ParsePerfdata(pre)
		fmt.Println("perfdata --- ", perfdata)
	}

	cmdOut := tempOutput[0]
	level.Info(logger).Log("command_output", cmdOut)
	if len(perfdata) > 0 {
		level.Info(logger).Log("perfdata", perfdata)
	} else {
		level.Info(logger).Log("msg", "Command returned without perfdata")
	}

	//level.Info(logger).Log("msg", "Command returned", "command", cmd,
	level.Info(logger).Log("msg", "Command returned",
		"address", ipaddr, "duration", duration, "return_code", result.ResultCode,
		"command_output")
	statusOk := 1.0
	if result.ResultCode != 0 {
		statusOk = 0
	}
	return CommandResult{duration, statusOk, &result, perfdata}, nil
}

// Collect dials nrpe-server and issues given command, recording metrics based on the result.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	var ctx *openssl.Ctx
	var conn net.Conn
	var err error

	// Connect to NRPE server
	if c.ssl {
		ctx, err = openssl.NewCtx()
		if err != nil {
			level.Error(c.logger).Log("msg", "Error creating SSL context", "err", err)
			return
		}
		err = ctx.SetCipherList("ALL:!MD5:@STRENGTH")
		if err != nil {
			level.Error(c.logger).Log("msg", "Error setting SSL cipher list", "err", err)
			return
		}
		conn, err = openssl.Dial("tcp", c.target, ctx, openssl.InsecureSkipHostVerification)
		if conn == (*openssl.Conn)(nil) || err != nil {
			level.Error(c.logger).Log("msg", "Error dialing NRPE server", "err", err)
			return
		}
	} else {
		d := net.Dialer{}
		conn, err = d.Dial("tcp", c.target)
		if err != nil {
			level.Error(c.logger).Log("msg", "Error dialing NRPE server", "err", err)
			return
		}
	}
	defer conn.Close()

	cmdResult, err := collectCommandMetrics(c.command, conn, c.logger)
	if err != nil {
		level.Error(c.logger).Log("msg", "Error running command", "command", c.command, "err", err)
		return
	}

	// Create metrics based on results of given command
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("command_duration", "Length of time the NRPE command took", nil, nil),
		prometheus.GaugeValue,
		cmdResult.commandDuration,
	)
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("command_ok", "Indicates whether or not the command was a success", nil, nil),
		prometheus.GaugeValue,
		cmdResult.statusOk,
	)
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("command_status", "Indicates the status of the command", nil, nil),
		prometheus.GaugeValue,
		float64(cmdResult.result.ResultCode),
	)

	for key, value := range cmdResult.Perfdata {
		// convert value to float64
		floatValue, err := strconv.ParseFloat(value, 64)
		if err != nil {
			// handle error
			continue
		}

		ch <- prometheus.MustNewConstMetric(
			prometheus.NewDesc(c.prefix+key, "Length of time the NRPE command took", nil, nil),
			prometheus.GaugeValue,
			floatValue,
		)
	}

	// Make sure the connection is closed, since it will re-dial on the next check
	// Closing a connection more than once is fine. The defer above will simply noop, as it's already closed
	err = conn.Close()
	if err != nil {
		level.Error(c.logger).Log("msg", "Could not close connection to NRPE server", "target", c.target, "err", err)
	}
}

// NewCollector returns new collector with logger and given command
func NewCollector(command, target string, ssl bool, logger log.Logger, prefix string) *Collector {
	return &Collector{
		command: command,
		target:  target,
		ssl:     ssl,
		logger:  logger,
		prefix:  prefix,
	}
}

func handler(w http.ResponseWriter, r *http.Request, logger log.Logger) {
	params := r.URL.Query()
	target := params.Get("target")
	if target == "" {
		http.Error(w, "Target parameter is missing", 400)
		return
	}
	cmd := params.Get("command")
	if cmd == "" {
		http.Error(w, "Command parameter is missing", 400)
		return
	} else {
		cmd = strings.TrimSpace(cmd)
	}

	args := params.Get("args")
	parsedArgs := strings.Split(args, "::")
	if len(parsedArgs) > 1 {
		args = strings.Join(parsedArgs, " ")
	}
	logger.Log("msg", "Received request", "command", cmd, "target", target, "args", args)

	if args != "" {
		cmd = cmd + "!" + args
		logger.Log("msg", "Arguments provided for command", "command", cmd)
	} else {
		logger.Log("msg", "No arguments provided for command", "command", cmd)
	}

	sslParam := params.Get("ssl")
	ssl := sslParam == "true"

	prefix := params.Get("prefix")

	registry := prometheus.NewRegistry()
	collector := NewCollector(cmd, target, ssl, logger, prefix)
	registry.MustRegister(collector)
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

func main() {
	logConfig := promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, &logConfig)
	kingpin.Version(version.Print("nrpe_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logger := promlog.New(&logConfig)
	level.Info(logger).Log("msg", "Starting nrpe_exporter", "version", version.Info())
	level.Info(logger).Log("msg", "Build context", "build_context", version.BuildContext())
	level.Info(logger).Log("msg", "Listening on address", "address", *listenAddress)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
            <head>
            <title>NRPE Exporter</title>
            </head>
            <body>
            <h1>NRPE Exporter</h1>
						<p><a href="/metrics">Metrics</a></p>
						<p><a href="/export?command=check_load&target=nrpe:5666">check_load against localhost:5666</a></p>
            </body>
            </html>`))
	})

	http.HandleFunc("/export", func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, logger)
	})
	http.Handle("/metrics", promhttp.Handler())
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		level.Error(logger).Log("msg", "Error starting HTTP server")
		os.Exit(1)
	}
	level.Info(logger).Log("msg", "Listening on address", "address", *listenAddress)
}
