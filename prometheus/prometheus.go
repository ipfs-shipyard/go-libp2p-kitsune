package prometheus

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("prometheus")

var (
	CurrentUpstreamPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "proxy",
		Subsystem: "bitswap",
		Name:      "upstream_peers",
		Help:      "The current number of upstream peers",
	})
	CurrentDownstreamPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "proxy",
		Subsystem: "bitswap",
		Name:      "downstream_peers",
		Help:      "The current number of downstream peers",
	})
	TotalBitswapMessagesRecv = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "proxy",
		Subsystem: "bitswap",
		Name:      "total_messages_recv",
		Help:      "The total number of Bitswap messages received",
	})
	TotalBitswapMessagesSent = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "proxy",
		Subsystem: "bitswap",
		Name:      "total_messages_sent",
		Help:      "The total number of Bitswap messages sent",
	})
	BitswapMessagesRecv = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "proxy",
		Subsystem: "bitswap",
		Name:      "messages_recv",
		Help:      "The number of Bitswap messages received per peerID",
	}, []string{"peer"})
	BitswapMessagesSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "proxy",
		Subsystem: "bitswap",
		Name:      "messages_sent",
		Help:      "The number of Bitswap messages sent per peerID",
	}, []string{"peer"})
	// TODO Should we only keep stats for downstream peers? There will be a LOT of upstream
	//      peers here
	BlockRTTms = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "proxy",
		Subsystem: "bitswap",
		Name:      "block_rtt_ms",
		Help:      "Milliseconds from sending a WANT to a peer to receiving the corresponding BLOCK",
		Buckets:   []float64{250, 500, 1000, 2000, 3000, 5000, 10000, 30000, 60000},
	}, []string{"peer"})
)

func StartPrometheus(port uint16) {
	portStr := fmt.Sprintf(":%v", port)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	go func() {
		err := http.ListenAndServe(portStr, mux)

		if err != nil {
			log.Errorf("Error starting Prometheus listener: %s", err)
			return
		}
	}()

	log.Infof("Prometheus server started on port %v", port)
}
