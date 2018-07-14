package config

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/etcdserver/etcdserverpb"
	"github.com/coreos/etcd/mvcc/mvccpb"
	. "github.com/onsi/gomega"
	net_context "golang.org/x/net/context"
)

type etcdClientCall struct {
	revision  int64
	keyPrefix string
	operation string //GET or WATCH
}

type mockEtcdClient struct {
	getResponses          []*clientv3.GetResponse
	watchResponseChannels []chan clientv3.WatchResponse
	observedCalls         []etcdClientCall
}

func (m *mockEtcdClient) Close() error {
	return nil
}

func (m *mockEtcdClient) Get(ctx net_context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	revision := int64(-1)
	ret := clientv3.OpGet(key, opts...)
	revision = ret.MinModRev()
	m.observedCalls = append(m.observedCalls, etcdClientCall{revision: revision, keyPrefix: key, operation: "GET"})
	response := m.getResponses[0]
	m.getResponses = m.getResponses[1:]
	return response, nil
}

func (m *mockEtcdClient) Watch(ctx net_context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
	revision := int64(-1)
	ret := clientv3.OpGet(key, opts...) //opWatch is not exposed by etcd
	revision = ret.Rev()
	m.observedCalls = append(m.observedCalls, etcdClientCall{revision: revision, keyPrefix: key, operation: "WATCH"})
	watchResponseCh := m.watchResponseChannels[0]
	m.watchResponseChannels = m.watchResponseChannels[1:]
	return watchResponseCh
}

func buildWatchResponse(headerRevision int64, compactRevision int64, key string, value string) clientv3.WatchResponse {
	return clientv3.WatchResponse{
		Header: etcdserverpb.ResponseHeader{
			Revision: headerRevision,
		},
		Events: []*clientv3.Event{
			{
				Kv: &mvccpb.KeyValue{
					Key:   []byte(key),
					Value: []byte(value),
				},
			},
		},
		CompactRevision: compactRevision,
	}
}

func buildGetResponse(revision int64, key string, value string) *clientv3.GetResponse {
	return &clientv3.GetResponse{
		Header: &etcdserverpb.ResponseHeader{Revision: revision},
		Kvs: []*mvccpb.KeyValue{
			{
				Key:   []byte(key),
				Value: []byte(value),
			},
		},
	}
}

func TestWatch(t *testing.T) {
	RegisterTestingT(t)

	type etcdEvent struct {
		key       string
		value     string
		operation string
	}

	watchResponses := []clientv3.WatchResponse{}
	watchResponseCount := 10
	watchCompactedResponseIndex := 4
	for revision := 1; revision <= watchResponseCount; revision++ {
		if revision == watchCompactedResponseIndex {
			watchResponses = append(watchResponses, buildWatchResponse(int64(revision), 5, "", ""))
		} else {
			watchResponses = append(watchResponses, buildWatchResponse(int64(revision), 0, fmt.Sprintf("/%d", revision), ""))
		}
	}

	observedEvents := []etcdEvent{}
	RecordPutEvents := func(key []byte, value []byte) {
		observedEvents = append(observedEvents, etcdEvent{key: string(key), value: string(value), operation: "PUT"})
	}

	RecordDeleteEvents := func(key []byte) {
		observedEvents = append(observedEvents, etcdEvent{key: string(key), value: "", operation: "DELETE"})
	}

	ctx, watchCancel := context.WithCancel(context.Background())
	watchRespChannelBeforeCompaction := make(chan clientv3.WatchResponse, watchResponseCount)
	watchRespChannelAfterCompaction := make(chan clientv3.WatchResponse, watchResponseCount)
	mock := mockEtcdClient{
		getResponses:          nil,
		watchResponseChannels: []chan clientv3.WatchResponse{watchRespChannelBeforeCompaction, watchRespChannelAfterCompaction},
		observedCalls:         nil,
	}
	for _, etcdResponse := range watchResponses {
		watchRespChannelBeforeCompaction <- etcdResponse
	}
	w := EtcdWatcher{
		watchPrefix:   "/",
		backOffTime:   time.Millisecond,
		client:        &mock,
		putHandler:    RecordPutEvents,
		deleteHandler: RecordDeleteEvents,
	}
	// Should return after watchCompactedResponseIndex - watch revision compacted
	rev, err := w.Watch(ctx, 0)
	Expect(rev).To(Equal(int64(watchCompactedResponseIndex - 1)))
	Expect(err).To(Equal(watchResponses[watchCompactedResponseIndex-1].Err()))
	Expect(len(observedEvents)).To(Equal(watchCompactedResponseIndex - 1))
	close(watchRespChannelBeforeCompaction)

	// Recreate watch from here to make sure it processes all events
	observedEvents = observedEvents[:0]
	for _, etcdResponse := range watchResponses[watchCompactedResponseIndex:] {
		watchRespChannelAfterCompaction <- etcdResponse
	}
	close(watchRespChannelAfterCompaction)
	w = EtcdWatcher{
		watchPrefix:   "/",
		backOffTime:   1,
		client:        &mock,
		putHandler:    RecordPutEvents,
		deleteHandler: RecordDeleteEvents,
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rev, err = w.Watch(ctx, 4)
	}()
	time.AfterFunc(5*time.Second, watchCancel)
	wg.Wait()
	Expect(rev).To(Equal(int64(watchResponseCount)))
	Expect(len(observedEvents)).To(Equal(watchResponseCount - watchCompactedResponseIndex))
}

/*
Set modRev = 2 in initial Get response     - 1 PUT event
Watch should start at version 3
Watch compacted at version 5               - 2 PUT events
Get req with minmodver = 5                 - 1 PUT event
Get response with modRev = 8
Watch starts at 9 again
Watch stops at channel closed error rev 10 - 2 PUT event
Get again returns modrev 13                - 1 PUT event
Watch again at rev  14
Wacth cancelled - end of test              - 2 PUT event
*/
func TestResyncAndWatch(t *testing.T) {
	RegisterTestingT(t)

	type etcdEvent struct {
		key       string
		value     string
		operation string
	}

	const (
		initialGetResponseModRev    int64 = 2
		expectedWatchStartRev       int64 = 3
		watchCompactedRevision      int64 = 5
		watchCompactedResponseIndex int64 = 8
		channelClosedRevision       int64 = 10
		getRevAfterChannelClosed    int64 = 13
		lastResponseRevision        int64 = 15
	)

	getResponses := []*clientv3.GetResponse{
		buildGetResponse(initialGetResponseModRev, fmt.Sprintf("/%d", 2), ""),
		buildGetResponse(watchCompactedResponseIndex, fmt.Sprintf("/%d", watchCompactedRevision), ""),
		buildGetResponse(getRevAfterChannelClosed, fmt.Sprintf("/%d", getRevAfterChannelClosed), ""),
	}

	watchResponses := []clientv3.WatchResponse{}
	for revision := expectedWatchStartRev; revision <= watchCompactedRevision; revision++ {
		if revision == watchCompactedRevision {
			watchResponses = append(watchResponses, buildWatchResponse(revision, int64(watchCompactedResponseIndex), "", ""))
		} else {
			watchResponses = append(watchResponses, buildWatchResponse(revision, 0, fmt.Sprintf("/%d", revision), ""))
		}
	}

	watchResponsesAfterCompaction := []clientv3.WatchResponse{}
	for revision := watchCompactedResponseIndex + 1; revision <= channelClosedRevision; revision++ {
		watchResponsesAfterCompaction = append(watchResponsesAfterCompaction, buildWatchResponse(int64(revision), 0, fmt.Sprintf("/%d", revision), ""))
	}

	watchResponsesAfterChannelClosed := []clientv3.WatchResponse{}
	for revision := getRevAfterChannelClosed + 1; revision <= lastResponseRevision; revision++ {
		watchResponsesAfterChannelClosed = append(watchResponsesAfterChannelClosed, buildWatchResponse(int64(revision), 0, fmt.Sprintf("/%d", revision), ""))
	}

	expectedEtcdCalls := []etcdClientCall{
		etcdClientCall{revision: 0, keyPrefix: "/", operation: "GET"},
		etcdClientCall{revision: expectedWatchStartRev, keyPrefix: "/", operation: "WATCH"},
		etcdClientCall{revision: watchCompactedRevision - 1, keyPrefix: "/", operation: "GET"},
		etcdClientCall{revision: watchCompactedResponseIndex + 1, keyPrefix: "/", operation: "WATCH"},
		etcdClientCall{revision: channelClosedRevision, keyPrefix: "/", operation: "GET"},
		etcdClientCall{revision: getRevAfterChannelClosed + 1, keyPrefix: "/", operation: "WATCH"},
	}

	observedEvents := []etcdEvent{}
	RecordPutEvents := func(key []byte, value []byte) {
		observedEvents = append(observedEvents, etcdEvent{key: string(key), value: string(value), operation: "PUT"})
	}

	RecordDeleteEvents := func(key []byte) {
		observedEvents = append(observedEvents, etcdEvent{key: string(key), value: "", operation: "DELETE"})
	}

	ctx, watchCancel := context.WithCancel(context.Background())

	watchRespChannelBeforeCompaction := make(chan clientv3.WatchResponse, 10)
	watchRespChannelAfterCompaction := make(chan clientv3.WatchResponse, 10)
	watchRespChannelAfterChannelClosed := make(chan clientv3.WatchResponse, 10)
	mock := mockEtcdClient{
		getResponses:          getResponses,
		watchResponseChannels: []chan clientv3.WatchResponse{watchRespChannelBeforeCompaction, watchRespChannelAfterCompaction, watchRespChannelAfterChannelClosed},
		observedCalls:         nil,
	}
	for _, watchResponse := range watchResponses {
		mock.watchResponseChannels[0] <- watchResponse
	}
	close(mock.watchResponseChannels[0]) // Processing should stop at watch compacted response. Channel close event should not be received

	for _, watchResponse := range watchResponsesAfterCompaction {
		mock.watchResponseChannels[1] <- watchResponse
	}
	close(mock.watchResponseChannels[1]) // Processing should stop at channel close error

	for _, watchResponse := range watchResponsesAfterChannelClosed {
		mock.watchResponseChannels[2] <- watchResponse
	}
	defer close(mock.watchResponseChannels[2]) // Processing should stop at context cancel here at test

	w := EtcdWatcher{
		watchPrefix:   "/",
		backOffTime:   1,
		client:        &mock,
		putHandler:    RecordPutEvents,
		deleteHandler: RecordDeleteEvents,
	}
	var wg sync.WaitGroup
	rev := int64(-1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		rev, _ = w.ResyncAndWatch(ctx, 0)
	}()
	time.AfterFunc(5*time.Second, watchCancel)
	wg.Wait()
	Expect(rev).To(Equal(lastResponseRevision))
	Expect(len(observedEvents)).To(Equal(9))
	Expect(mock.observedCalls).To(Equal(expectedEtcdCalls))
}
