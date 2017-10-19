```
$ mkdir -p ${GOPATH}/src/github.com/coreswitch
$ cd ${GOPATH}/src/github.com/coreswitch
$ git clone git@github.com:coreswitch/openconfigd.git
$ cd openconfigd
$ git checkout -b quagga origin/quagga
```

```
$ go get github.com/coreswitch/zebra/rib/ribd
$ go get github.com/coreswitch/openconfigd/openconfigd
$ go get github.com/coreswitch/openconfigd/quagga/quaggad
```

```
$ sudo ${GOPATH}/bin/openconfigd -y ${GOPATH}/src/github.com/coreswitch/openconfigd/yang &
$ sudo ${GOPATH}/bin/ribd &
$ sudo /usr/lib/quagga/bgpd --config_file /dev/null --pid_file /tmp/bgpd.pid --socket /var/run/zserv.api &
$ sudo ${GOPATH}/bin/quaggad &
```
