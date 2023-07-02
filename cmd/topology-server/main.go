package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/run-ai/fake-gpu-operator/internal/common/kubeclient"
	"github.com/run-ai/fake-gpu-operator/internal/common/topology"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/api/errors"
)

func main() {
	kubeclient := kubeclient.NewKubeClient(nil, nil)
	viper.SetDefault("TOPOLOGY_CM_NAME", os.Getenv("TOPOLOGY_CM_NAME"))
	viper.SetDefault("TOPOLOGY_CM_NAMESPACE", os.Getenv("TOPOLOGY_CM_NAMESPACE"))
	http.HandleFunc("/topology", func(w http.ResponseWriter, r *http.Request) {
		cm, ok := kubeclient.GetConfigMap(os.Getenv("TOPOLOGY_CM_NAMESPACE"), os.Getenv("TOPOLOGY_CM_NAME"))
		if !ok {
			panic("Can't get topology")
		}
		baseTopology, err := topology.FromBaseTopologyCM(cm)
		if err != nil {
			panic(err)
		}

		baseTopologyJSON, err := json.Marshal(baseTopology)
		if err != nil {
			panic(err)
		}

		log.Printf("Returning cluster topology: %s", baseTopologyJSON)

		w.Header().Set("Content-Type", "application/json")
		_, err = w.Write(baseTopologyJSON)
		if err != nil {
			panic(err)
		}
	})

	http.HandleFunc("/topology/nodes/", func(w http.ResponseWriter, r *http.Request) {
		nodeName := strings.Split(r.URL.Path, "/")[3]
		w.Header().Set("Content-Type", "application/json")

		if nodeName == "" {
			panic("Can't get node name from url " + r.URL.Path)
		}
		nodeTopology, err := topology.GetNodeTopologyFromCM(kubeclient.ClientSet, nodeName)
		if err != nil {
			if errors.IsNotFound(err) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("Node topology not found"))
			} else {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(err.Error()))
			}
			return
		}

		nodeTopologyJSON, err := json.Marshal(nodeTopology)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		log.Printf("Returning node topology: %s", nodeTopologyJSON)

		_, err = w.Write(nodeTopologyJSON)
		if err != nil {
			panic(err)
		}
	})

	log.Printf("Serving on port 8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
