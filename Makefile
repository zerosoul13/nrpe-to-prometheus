PHONY: buildall

VERSION=0.0.1

buildall: BUILDNRPE-EXPORTER BUILDNRPE

build-nrpe-exporter:
	- docker build --build-arg ARCH=arm64  -f ./nrpe_exporter/Dockerfile -t nrpe-exporter:${VERSION} ./nrpe_exporter

build-nrpe:
	- docker build --build-arg ARCH=arm64 -f ./docker-nrpe/Dockerfile -t docker-nrpe:${VERSION} ./docker-nrpe

remove-all-images:
	- docker rmi nrpe-exporter:${VERSION}
	- docker rmi docker-nrpe:${VERSION}

stop-all-containers:
	- docker-compose down

start-all-containers:
	- docker-compose up -d