package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type RIB struct {
	Prefix  string
	Nexthop string
	Stale   bool
}

var ribMap = map[string]*RIB{}

var nh2tunnelMap = map[string]string{
	"192.168.30.11": "tif1-0",
	"192.168.30.12": "tif2-0",
	"192.168.30.13": "tif3-0",
}

func Dump() {
	fmt.Println("------")
	for key, value := range ribMap {
		fmt.Println(key, "=>", value)
	}
	fmt.Println("------")
}

func Mark() {
	for _, value := range ribMap {
		value.Stale = true
	}
}

func Update(prefix, nexthop string) {
	fmt.Println("UPDATE", prefix, nexthop)
	rib := &RIB{
		Prefix:  prefix,
		Nexthop: nexthop,
		Stale:   false,
	}
	ribMap[prefix] = rib
}

func Sync() {
	var ribMapNew = map[string]*RIB{}
	for prefix, value := range ribMap {
		// Set add or del.
		op := ""
		if !value.Stale {
			op = "add"
			ribMapNew[prefix] = value
		} else {
			op = "del"
		}
		fmt.Println(op, prefix, value)

		// Look up tunnel interface
		tif := nh2tunnelMap[value.Nexthop]
		if tif == "" {
			continue
		}

		// sudo ip route add 172.16.120.0/24 dev tif0-0 table 1
		args := []string{"route", op, prefix, "dev", tif, "table", "1"}

		// Update kernel routing table.
		exec.Command("ip", args...).Run()
		fmt.Println(args, value.Nexthop, tif)
	}
	ribMap = ribMapNew
}

func Fetch() {
	// Exec "gobgp vrf vrf1 rib" command then get return string.
	out, err := exec.Command("gobgp", "vrf", "vrf1", "rib").Output()
	if err != nil {
		fmt.Println(err)
		return
	}

	// Split the line.
	Mark()
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		columns := strings.Fields(line)
		if len(columns) > 2 && columns[0] == "*>" {
			Update(columns[1], columns[2])
		}
	}
	Dump()
	Sync()
	Dump()
}

func main() {
	for {
		Fetch()
		time.Sleep(time.Second)
	}
}
