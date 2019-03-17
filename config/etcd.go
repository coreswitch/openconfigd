// Copyright 2016 OpenConfigd Project.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
)

type JsonBody struct {
	Value   string
	Body    string `mapstructure:"body"`
	Version int    `mapstructure:"version"`
}

var (
	// `etcd' endpoints as described in etcdctl help: --endpoints value a
	// comma-delimited list of machine addresses in the cluster (default:
	// "http://127.0.0.1:2379,http://127.0.0.1:4001")
	etcdEndpoints []string

	// etcd watch path.
	etcdPath string

	// Last etcd value.
	etcdLastValue string

	// Last etcd value as JSON.
	etcdLastJson    JsonBody
	etcdLastVrfJson JsonBody
	etcdBgpWanJson  JsonBody

	// Value map.
	etcdKeyValueMap = map[string]*JsonBody{}

	// Mutex for serializing etcd event handling
	EtcdEventMutex sync.RWMutex

	etcdWatchHandler         *EtcdWatcher
	etcdWatchContextCanceler func()
	etcdWatchWg              sync.WaitGroup
)

const (
	etcdDialTimeout = 3 * time.Second
	etcdBackOffTime = 3 * time.Second
)

func EtcdWatchStop() {
	if etcdWatchContextCanceler != nil {
		log.Info("Stopping etcd watcher")
		etcdWatchContextCanceler()
		etcdWatchWg.Wait()
		etcdWatchHandler = nil
		log.Info("Stopped etcd watcher")
	}
}

func EtcdWatchUpdate() {
	// Stop current etcd watch and start a new watch with the new endpoint/prefix
	log.Infof("Update etcd watch")
	EtcdEventMutex.Lock()
	defer EtcdEventMutex.Unlock()

	EtcdWatchStop()
	ClearVrfCache() // Force resync ribd

	if etcdPath == "" {
		return
	}

	// No endpoints just return.
	if len(etcdEndpoints) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	etcdWatchContextCanceler = cancel

	etcdWatchWg.Add(1)
	go func(etcdEndpoints []string, etcdPath string, etcdPutRequestHandler func([]byte, []byte), etcdDeleteHandler func([]byte)) {
		var startRevision int64 = 0
		defer etcdWatchWg.Done()
		for {
			var err error
			etcdWatchHandler, err = NewWatcher(etcdEndpoints, etcdDialTimeout, etcdPath, etcdBackOffTime, etcdPutRequestHandler, etcdDeleteHandler)
			if err != nil {
				log.Warningf("Creating a new watcher failed. Error: %s", err)
			} else {
				log.Infof("Etcd Resync and watch")
				startRevision, err = etcdWatchHandler.ResyncAndWatch(ctx, startRevision)
				if err != nil {
					log.Warningf("Etcd Resync and watch returned error: %s", err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(etcdBackOffTime):
			}
		}
	}(etcdEndpoints, etcdPath, EtcdKeyValueParse, EtcdKeyDelete)
}

// Add etcd endpoint.
func EtcdEndpointsAdd(endPoint string) {
	etcdEndpoints = append(etcdEndpoints, endPoint)
	EtcdWatchUpdate()
}

// Delete etcd endpoint.
func EtcdEndpointsDelete(endPoint string) {
	EndPoints := []string{}
	for _, ep := range etcdEndpoints {
		if ep != endPoint {
			EndPoints = append(EndPoints, ep)
		}
	}
	etcdEndpoints = EndPoints
	EtcdWatchUpdate()
}

func EtcdEndpointsShow() (str string) {
	str = "Etcd Path Config: " + etcdPath + "\n"
	if etcdWatchContextCanceler == nil {
		if etcdPath == "" {
			str += "Etcd Status: etcd path is not configured\n"
		}
		if len(etcdEndpoints) == 0 {
			str += "Etcd Status: etcd endpoint is not configured\n"
		}
		return
	}

	for _, endPoint := range etcdEndpoints {
		str += "Etcd Endpoints: " + endPoint + "\n"
	}
	if etcdPath != "" {
		str += "Etcd Watch Path: " + etcdPath + "\n"
	}
	return
}

// TODO: The design for individual component key path is not good
// Ideally we want to watch only a certain prefix, not all
func getFunctionalityPathFromKey(keyStr string) ([]string, bool) {
	// /config/services/* or /local/services/* or /config/devices/<uuid>/services/bgp
	// * is one of bgp, vrf, bgp_wan, quagga, command

	local := false

	splitFunc := func(c rune) bool {
		return c == '/'
	}
	path := strings.FieldsFunc(keyStr, splitFunc)

	if len(path) < 3 {
		return []string{}, local
	}

	if path[0] == "local" {
		local = true
	} else if path[0] != "config" {
		return []string{}, local
	}

	path = path[1:]

	if path[0] == "devices" {
		if len(path) < 4 {
			return []string{}, local
		} else {
			path = path[2:]
		}
	}

	if path[0] != "services" {
		return []string{}, local
	}

	path = path[1:]

	if path[0] != "bgp" && path[0] != "vrf" && path[0] != "bgp_wan" && path[0] != "quagga" && path[0] != "command" {
		return []string{}, local
	}

	return path, local
}

func EtcdKeyValueParse(key []byte, value []byte) {
	// Validate and get the required path
	path, local := getFunctionalityPathFromKey(string(key))
	if len(path) < 1 {
		return
	}

	jsonStr := string(value)
	fmt.Println("jsonStr", jsonStr)

	var jsonIntf interface{}
	err := json.Unmarshal(value, &jsonIntf)
	if err != nil {
		log.WithFields(log.Fields{
			"json":  string(value),
			"error": err,
			"path":  string(key),
			"value": string(value),
		}).Error("EtcdKeyValueParse:json.Unmarshal()")
		return
	}

	jsonBody := &JsonBody{}
	err = mapstructure.Decode(jsonIntf, jsonBody)
	if err == nil && jsonBody.Body != "" {
		jsonStr = jsonBody.Body
	}

	// Cache for debug command
	keyStr := string(key)
	etcdKeyValueMap[keyStr] = jsonBody

	log.Infof("[etcd] Put: %s", keyStr)

	switch path[0] {
	case "bgp":
		etcdLastValue = string(value)
		//etcdLastJson = *jsonBody
		if len(path) == 3 {
			GobgpNeighborAdd(path[2], jsonStr)
		} else {
			GobgpParse(jsonStr)
		}
	case "quagga":
		if len(path) <= 1 {
			return
		}
		etcdLastValue = string(value)
		etcdLastJson = *jsonBody
		vrfId, err := strconv.Atoi(path[1])
		if err != nil {
			log.Errorf("%s is not a valud vrf ID", path[1])
		}
		QuaggaConfigSync(etcdLastValue, vrfId, "local")
	case "vrf":
		if len(path) <= 1 {
			return
		}
		etcdLastVrfJson = *jsonBody
		vrfId, err := strconv.Atoi(path[1])
		if err != nil {
			log.Errorf("%s is not a valud vrf ID", path[1])
		}
		VrfParse(vrfId, jsonBody.Body)
	case "bgp_wan":
		etcdBgpWanJson = *jsonBody
		GobgpWanParse(jsonBody.Body, local)
	case "command":
		switch path[1] {
		case "dhcp":
			DhcpStatusUpdate()
		case "bgp_lan":
			QuaggaStatusUpdate()
		case "bgp_wan":
			GobgpStatusUpdate()
		case "ospf":
			OspfStatusUpdate()
		}
	}
}

func EtcdKeyDelete(key []byte) {
	keyStr := string(key)
	delete(etcdKeyValueMap, keyStr) // Delete from local cache

	// Path of subscription.
	path, local := getFunctionalityPathFromKey(keyStr)
	if len(path) < 1 {
		return
	}

	log.Infof("[etcd] Path Delete: %s", keyStr)

	switch path[0] {
	case "bgp":
		if len(path) == 3 {
			GobgpNeighborDelete(path[2])
		}
	case "vrf":
		vrfId := 0
		if len(path) > 1 {
			vrfId, _ = strconv.Atoi(path[1])
		}
		if vrfId == 0 {
			return
		}
		if len(path) > 2 && path[2] == "bgp" {
			QuaggaDelete(vrfId)
		} else {
			VrfDelete(vrfId, true)
		}
	case "bgp_wan":
		GobgpWanStop(local)
	case "quagga":
		vrfId := 0
		if len(path) > 1 {
			vrfId, _ = strconv.Atoi(path[1])
		}
		if vrfId == 0 {
			return
		}
		ProcessQuaggaConfigDelete(vrfId, "local")
	}
}

func EtcdDeletePath(etcdEndpoints []string, etcdPath string) {
	cfg := clientv3.Config{
		Endpoints:   etcdEndpoints,
		DialTimeout: 3 * time.Second,
	}
	conn, err := clientv3.New(cfg)
	if err != nil {
		log.Errorf("EtcdSetJson clientv3.New error: ", err)
		return
	}
	defer conn.Close()

	_, err = conn.Delete(context.Background(), etcdPath)
	if err != nil {
		log.Warningf("EtcdDeletePath conn delete failed. Error: %s", err)
		return
	}
}

func EtcdLock(etcdClient clientv3.Client, lockPath string, myID string, block bool) (error, clientv3.LeaseID) {
	for {
		var err error
		var masterTTL int64 = 10
		ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
		lease, err := etcdClient.Grant(ctx, masterTTL)
		var resp *clientv3.TxnResponse
		ctx, _ = context.WithTimeout(context.Background(), 10*time.Second)
		if err == nil {
			cmp := clientv3.Compare(clientv3.CreateRevision(lockPath), "=", 0)
			put := clientv3.OpPut(lockPath, myID, clientv3.WithLease(lease.ID))
			resp, err = etcdClient.Txn(ctx).If(cmp).Then(put).Commit()
		}
		if err != nil || !resp.Succeeded {
			msg := fmt.Sprintf("failed to lock path %s", lockPath)
			if err != nil {
				msg = fmt.Sprintf("failed to lock path %s: %s", lockPath, err)
			}
			log.Error(msg)
			if !block {
				return errors.New(msg), lease.ID
			}
			time.Sleep(time.Duration(masterTTL) * time.Second)
			continue
		}
		log.Debug("Locked %s", lockPath)
		return nil, lease.ID
	}
}

func EtcdUnlock(etcdClient clientv3.Client, lockPath string, leaseID clientv3.LeaseID) error {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	etcdClient.Revoke(ctx, leaseID)
	log.Debug("Unlocked path %s", lockPath)
	return nil
}

// Show function
func showSystemEtcd(Args []string) (inst int, instStr string) {
	inst = CliSuccessShow
	instStr = EtcdEndpointsShow()
	return
}

func EtcdPathApi(set bool, Args []interface{}) {
	if len(Args) != 1 {
		return
	}
	path := Args[0].(string)
	if set {
		etcdPath = path
	} else {
		etcdPath = ""
	}
	EtcdWatchUpdate()
}

func EtcdEndpointsApi(set bool, Args []interface{}) {
	if len(Args) != 1 {
		return
	}
	endPoint := Args[0].(string)
	if set {
		EtcdEndpointsAdd(endPoint)
	} else {
		EtcdEndpointsDelete(endPoint)
	}
}

func configureEtcdJsonFunc(Args []string) (inst int, instStr string) {
	inst = CliSuccessShow
	instStr = etcdLastValue
	return
}

func configureEtcdBodyFunc(Args []string) (inst int, instStr string) {
	inst = CliSuccessShow
	instStr = etcdLastJson.Body
	return
}

func configureEtcdVersionFunc(Args []string) (inst int, instStr string) {
	inst = CliSuccessShow
	instStr = strconv.Itoa(etcdLastJson.Version)
	return
}

func configureEtcdBodyFunc2(Args []string) (inst int, instStr string) {
	inst = CliSuccessShow
	instStr = etcdLastVrfJson.Body
	return
}

func configureEtcdVersionFunc2(Args []string) (inst int, instStr string) {
	inst = CliSuccessShow
	instStr = strconv.Itoa(etcdLastVrfJson.Version)
	return
}

func configureEtcdBgpWanBodyFunc(Args []string) (inst int, instStr string) {
	inst = CliSuccessShow
	instStr = etcdBgpWanJson.Body
	return
}

func configureEtcdBgpConfigFunc(Args []string) (inst int, instStr string) {
	inst = CliSuccessShow
	byte, _ := json.Marshal(gobgpConfig)
	instStr = string(byte)
	return
}
