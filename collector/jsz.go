// Copyright 2021 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package collector has various collector utilities and implementations.
package collector

import (
	"net/http"
	"sync"
	"time"

	nats "github.com/nats-io/nats-server/v2/server"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	jszSuffix = "/jsz?consumers=true"
)

type jszCollector struct {
	sync.Mutex
	httpClient *http.Client
	servers    []*CollectedServer
	system     string

	// JetStream server stats
	streams   *prometheus.Desc
	consumers *prometheus.Desc
	messages  *prometheus.Desc
	bytes     *prometheus.Desc

	// Stream stats
	streamMessages      *prometheus.Desc
	streamBytes         *prometheus.Desc
	streamLastSeq       *prometheus.Desc
	streamConsumerCount *prometheus.Desc

	// Consumer stats
	consumerDeliveredConsumerSeq *prometheus.Desc
	consumerDeliveredStreamSeq   *prometheus.Desc
	consumerNumAckPending        *prometheus.Desc
	consumerNumRedelivered       *prometheus.Desc
	consumerNumWaiting           *prometheus.Desc
	consumerNumPending           *prometheus.Desc
}

func isJszEndpoint(system, endpoint string) bool {
	return system == JetStreamSystem
}

func newJszCollector(system, endpoint string, servers []*CollectedServer) prometheus.Collector {
	serverLabels := []string{"server_id", "cluster", "domain", "meta_leader"}

	var streamLabels []string
	streamLabels = append(streamLabels, serverLabels...)
	streamLabels = append(streamLabels, "stream_name")
	streamLabels = append(streamLabels, "stream_leader")

	var consumerLabels []string
	consumerLabels = append(consumerLabels, streamLabels...)
	consumerLabels = append(consumerLabels, "consumer_name")
	consumerLabels = append(consumerLabels, "consumer_leader")
	consumerLabels = append(consumerLabels, "deliver_subject")

	nc := &jszCollector{
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		// jetstream_stream_total_messages
		streams: prometheus.NewDesc(
			prometheus.BuildFQName(system, "server", "total_streams"),
			"Total number of streams in JetStream",
			serverLabels,
			nil,
		),
		// jetstream_server_total_consumers
		consumers: prometheus.NewDesc(
			prometheus.BuildFQName(system, "server", "total_consumers"),
			"Total number of consumers in JetStream",
			serverLabels,
			nil,
		),
		// jetstream_server_total_messages
		messages: prometheus.NewDesc(
			prometheus.BuildFQName(system, "server", "total_messages"),
			"Total number of stored messages in JetStream",
			serverLabels,
			nil,
		),
		// jetstream_server_total_message_bytes
		bytes: prometheus.NewDesc(
			prometheus.BuildFQName(system, "server", "total_message_bytes"),
			"Total number of bytes stored in JetStream",
			serverLabels,
			nil,
		),
		// jetstream_stream_total_messages
		streamMessages: prometheus.NewDesc(
			prometheus.BuildFQName(system, "stream", "total_messages"),
			"Total number of messages from a stream",
			streamLabels,
			nil,
		),
		// jetstream_stream_total_bytes
		streamBytes: prometheus.NewDesc(
			prometheus.BuildFQName(system, "stream", "total_bytes"),
			"Total stored bytes from a stream",
			streamLabels,
			nil,
		),
		// jetstream_stream_state_last_seq
		streamLastSeq: prometheus.NewDesc(
			prometheus.BuildFQName(system, "stream", "last_seq"),
			"Last sequence from a stream",
			streamLabels,
			nil,
		),
		// jetstream_stream_consumer_count
		streamConsumerCount: prometheus.NewDesc(
			prometheus.BuildFQName(system, "stream", "consumer_count"),
			"Total number of consumers from a stream",
			streamLabels,
			nil,
		),
		// jetstream_consumer_delivered_consumer_seq
		consumerDeliveredConsumerSeq: prometheus.NewDesc(
			prometheus.BuildFQName(system, "consumer", "delivered_consumer_seq"),
			"Latest sequence number of a stream consumer",
			consumerLabels,
			nil,
		),
		// jetstream_consumer_delivered_stream_seq
		consumerDeliveredStreamSeq: prometheus.NewDesc(
			prometheus.BuildFQName(system, "consumer", "delivered_stream_seq"),
			"Latest sequence number of a stream",
			consumerLabels,
			nil,
		),
		// jetstream_consumer_num_ack_pending
		consumerNumAckPending: prometheus.NewDesc(
			prometheus.BuildFQName(system, "consumer", "num_ack_pending"),
			"Number of pending acks from a consumer",
			consumerLabels,
			nil,
		),
		// jetstream_consumer_num_redelivered
		consumerNumRedelivered: prometheus.NewDesc(
			prometheus.BuildFQName(system, "consumer", "num_redelivered"),
			"Number of redelivered messages from a consumer",
			consumerLabels,
			nil,
		),
		// jetstream_consumer_num_waiting
		consumerNumWaiting: prometheus.NewDesc(
			prometheus.BuildFQName(system, "consumer", "num_waiting"),
			"Number of inflight fetch requests from a pull consumer",
			consumerLabels,
			nil,
		),
		// jetstream_consumer_num_pending
		consumerNumPending: prometheus.NewDesc(
			prometheus.BuildFQName(system, "consumer", "num_pending"),
			"Number of pending messages from a consumer",
			consumerLabels,
			nil,
		),
	}

	nc.servers = make([]*CollectedServer, len(servers))
	for i, s := range servers {
		nc.servers[i] = &CollectedServer{
			ID:  s.ID,
			URL: s.URL + jszSuffix,
		}
	}

	return nc
}

// Describe shares the info description from a prometheus metric.
func (nc *jszCollector) Describe(ch chan<- *prometheus.Desc) {
	// Server state
	ch <- nc.streams
	ch <- nc.consumers
	ch <- nc.messages
	ch <- nc.bytes

	// Stream state
	ch <- nc.streamMessages
	ch <- nc.streamBytes
	ch <- nc.streamLastSeq
	ch <- nc.streamConsumerCount

	// Consumer state
	ch <- nc.consumerDeliveredConsumerSeq
	ch <- nc.consumerDeliveredStreamSeq
	ch <- nc.consumerNumAckPending
	ch <- nc.consumerNumRedelivered
	ch <- nc.consumerNumWaiting
	ch <- nc.consumerNumPending
}

// Collect gathers the server jsz metrics.
func (nc *jszCollector) Collect(ch chan<- prometheus.Metric) {
	for _, server := range nc.servers {
		var resp nats.JSInfo
		if err := getMetricURL(nc.httpClient, server.URL, &resp); err != nil {
			Debugf("ignoring server %s: %v", server.ID, err)
			continue
		}

		// JetStream Server Metrics
		var serverID, clusterName, jsDomain, clusterLeader string
		var streamName, streamLeader string
		var consumerName, consumerLeader string

		serverID = server.ID
		if resp.Meta != nil {
			clusterName = resp.Meta.Name
			clusterLeader = resp.Meta.Leader
		}
		jsDomain = resp.Config.Domain

		serverMetric := func(key *prometheus.Desc, value float64) prometheus.Metric {
			return prometheus.MustNewConstMetric(key, prometheus.GaugeValue, value,
				serverID, clusterName, jsDomain, clusterLeader)
		}
		ch <- serverMetric(nc.streams, float64(resp.Streams))
		ch <- serverMetric(nc.consumers, float64(resp.Consumers))
		ch <- serverMetric(nc.messages, float64(resp.Messages))
		ch <- serverMetric(nc.bytes, float64(resp.Bytes))

		for _, account := range resp.AccountDetails {
			for _, stream := range account.Streams {
				streamName = stream.Name
				if stream.Cluster != nil {
					streamLeader = stream.Cluster.Leader
				}
				streamMetric := func(key *prometheus.Desc, value float64) prometheus.Metric {
					return prometheus.MustNewConstMetric(key, prometheus.GaugeValue, value,
						// Server Labels
						serverID, clusterName, jsDomain, clusterLeader,
						// Stream Labels
						streamName, streamLeader)
				}
				ch <- streamMetric(nc.streamMessages, float64(stream.State.Msgs))
				ch <- streamMetric(nc.streamBytes, float64(stream.State.Bytes))
				ch <- streamMetric(nc.streamLastSeq, float64(stream.State.LastSeq))
				ch <- streamMetric(nc.streamConsumerCount, float64(stream.State.Consumers))

				// Now with the consumers.
				for _, consumer := range stream.Consumer {
					consumerName = consumer.Name
					if consumer.Cluster != nil {
						consumerLeader = consumer.Cluster.Leader
					}
					consumerMetric := func(key *prometheus.Desc, value float64) prometheus.Metric {
						return prometheus.MustNewConstMetric(key, prometheus.GaugeValue, value,
							// Server Labels
							serverID, clusterName, jsDomain, clusterLeader,
							// Stream Labels
							streamName, streamLeader,
							// Consumer Labels
							consumerName, consumerLeader, deliverSubject,
						)
					}
					ch <- consumerMetric(nc.consumerDeliveredConsumerSeq, float64(consumer.Delivered.Consumer))
					ch <- consumerMetric(nc.consumerDeliveredStreamSeq, float64(consumer.Delivered.Stream))
					ch <- consumerMetric(nc.consumerNumAckPending, float64(consumer.NumAckPending))
					ch <- consumerMetric(nc.consumerNumRedelivered, float64(consumer.NumRedelivered))
					ch <- consumerMetric(nc.consumerNumWaiting, float64(consumer.NumWaiting))
					ch <- consumerMetric(nc.consumerNumPending, float64(consumer.NumPending))
				}
			}
		}
	}
}
