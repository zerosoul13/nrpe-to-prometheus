#!/bin/sh

sed -i "s/^allowed_hosts=127.0.0.1$/allowed_hosts=127.0.0.1, ${NAGIOS_SERVER}/g" /etc/nagios/nrpe.cfg

# inject custom commands
echo "command[version]=/usr/lib/nagios/plugins/version" >> /etc/nagios/nrpe.cfg


/usr/sbin/nrpe -c /etc/nagios/nrpe.cfg -4 -n -d



# Wait for NRPE Daemon to exit
PID=$(ps -ef | grep -v grep | grep  "/usr/sbin/nrpe" | awk '{print $2}')
if [ ! "$PID" ]; then
  echo "Error: Unable to start nrpe daemon..."
  # exit 1
fi
while [ -d /proc/$PID ] && [ -z `grep zombie /proc/$PID/status` ]; do
    echo "NRPE: $PID (running)..."
    sleep 60s
done
echo "NRPE daemon exited. Quitting.."
