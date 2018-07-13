package config

import (
	"errors"
	"github.com/coreos/etcd/clientv3"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"time"
)

type etcdClient interface {
	Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error)
	Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan
	Close() error
}

type EtcdWatcher struct {
	client        etcdClient
	watchPrefix   string
	backOffTime   time.Duration
	putHandler    func([]byte, []byte)
	deleteHandler func([]byte)
}

func NewWatcher(endpoints []string, etcdDialTimeout time.Duration, watchPrefix string, backOffTime time.Duration, putHandler func([]byte, []byte), deleteHandler func([]byte)) (*EtcdWatcher, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: etcdDialTimeout,
	})
	if err != nil {
		return nil, err
	}

	if putHandler == nil || deleteHandler == nil {
		return nil, errors.New("Put Handler and Receive handlers are mandatory")
	}
	return &EtcdWatcher{
		watchPrefix:   watchPrefix,
		backOffTime:   backOffTime,
		client:        client,
		putHandler:    putHandler,
		deleteHandler: deleteHandler,
	}, nil
}

func (watcher *EtcdWatcher) Close() error {
	watcher.client.Close()
	return nil
}

func (watcher *EtcdWatcher) ResyncAndWatch(ctx context.Context, startRevision int64) (int64, error) {

	revision := startRevision
	// Get and Resync from version 0 initially. In case of watch error, say lease expiry or compaction,
	// get from the previous processed version and then start watch again
	for {
		log.Infof("ResyncAndWatch from revision: %d", revision)
		resp, err := watcher.client.Get(ctx, watcher.watchPrefix, clientv3.WithPrefix(), clientv3.WithMinModRev(revision), clientv3.WithSort(clientv3.SortByModRevision, clientv3.SortAscend))
		if err != nil {
			log.Error("Etcd Get Request failed in Resync. Error: %s", err)
			return revision, err
		}
		for _, ev := range resp.Kvs {
			watcher.putHandler(ev.Key, ev.Value)
		}
		revision = resp.Header.Revision + 1

		log.Infof("Start watch from version: %d", revision)
		revision, err = watcher.Watch(ctx, revision)

		log.Warningf("ETCD watch interrupted: %s", err)

		select {
		case <-ctx.Done():
			return revision, ctx.Err()
		case <-time.After(watcher.backOffTime):
		}
	}
}

func (watcher *EtcdWatcher) Watch(ctx context.Context, startRevision int64) (int64, error) {
	watchOpts := []clientv3.OpOption{}
	watchOpts = append(watchOpts, clientv3.WithPrefix())
	if startRevision != 0 {
		watchOpts = append(watchOpts, clientv3.WithRev(startRevision))
	}

	wCh := watcher.client.Watch(ctx, watcher.watchPrefix, watchOpts...)

	revision := startRevision
	for {
		select {
		case wresp, ok := <-wCh:
			if !ok {
				log.Errorf("Etcd Watch channel closed, ETCD revision: %d", revision)
				return revision, errors.New("Etcd Watch channel closed")
			}
			err := wresp.Err()
			if err != nil {
				log.Warning("Etcd response error : %s, ETCD revision: %d", err, revision)
				return revision, err
			}
			for _, ev := range wresp.Events {
				switch ev.Type {
				case clientv3.EventTypePut:
					watcher.putHandler(ev.Kv.Key, ev.Kv.Value)
				case clientv3.EventTypeDelete:
					watcher.deleteHandler(ev.Kv.Key)
				default:
					log.Warning("Unknown etcd event type: %d", ev.Type)
				}
			}
			revision = wresp.Header.Revision
		case <-ctx.Done():
			log.Warningf("ctx Done - Watch cancelled. Processed revision: %d", revision)
			return revision, nil
		}
	}
}
