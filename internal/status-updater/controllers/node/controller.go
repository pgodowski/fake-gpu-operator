package node

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/run-ai/fake-gpu-operator/internal/common/topology"
	"github.com/run-ai/fake-gpu-operator/internal/status-updater/controllers"
	"github.com/run-ai/fake-gpu-operator/internal/status-updater/controllers/util"

	nodehandler "github.com/run-ai/fake-gpu-operator/internal/status-updater/handlers/node"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type NodeController struct {
	kubeClient kubernetes.Interface
	wg         *sync.WaitGroup
	informer   cache.SharedIndexInformer
	handler    nodehandler.Interface
}

var _ controllers.Interface = &NodeController{}

func NewNodeController(kubeClient kubernetes.Interface, wg *sync.WaitGroup) *NodeController {
	c := &NodeController{
		kubeClient: kubeClient,
		wg:         wg,
		informer:   informers.NewSharedInformerFactory(kubeClient, 0).Core().V1().Nodes().Informer(),
		handler:    nodehandler.NewNodeHandler(kubeClient),
	}

	_, err := c.informer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch node := obj.(type) {
			case *v1.Node:
				return isFakeGpuNode(node)
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				node := obj.(*v1.Node)
				util.LogError(c.handler.HandleAdd(node), "Failed to handle node addition")
			},
			DeleteFunc: func(obj interface{}) {
				node := obj.(*v1.Node)
				util.LogError(c.handler.HandleDelete(node), "Failed to handle node deletion")
			},
		},
	})
	if err != nil {
		log.Fatalf("Failed to add node event handler: %v", err)
	}

	return c
}

func (c *NodeController) Run(stopCh <-chan struct{}) {
	defer c.wg.Done()

	err := c.pruneTopologyNodes()
	if err != nil {
		log.Fatalf("Failed to prune topology nodes: %v", err)
	}

	c.informer.Run(stopCh)
}

func (c *NodeController) pruneTopologyNodes() error {
	log.Print("Pruning topology nodes...")
	clusterTopology, err := topology.GetFromKube(c.kubeClient)
	if err != nil {
		return fmt.Errorf("failed getting cluster topology: %v", err)
	}

	gpuNodes, err := c.kubeClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: "nvidia.com/gpu.deploy.dcgm-exporter=true,nvidia.com/gpu.deploy.device-plugin=true",
	})
	if err != nil {
		return fmt.Errorf("failed listing fake gpu nodes: %v", err)
	}

	gpuNodesMap := make(map[string]bool)
	for _, node := range gpuNodes.Items {
		gpuNodesMap[node.Name] = true
	}

	for nodeName := range clusterTopology.Nodes {
		if _, ok := gpuNodesMap[nodeName]; !ok {
			delete(clusterTopology.Nodes, nodeName)
		}
	}

	err = topology.UpdateToKube(c.kubeClient, clusterTopology)
	if err != nil {
		return fmt.Errorf("failed updating cluster topology: %v", err)
	}

	return nil
}

func isFakeGpuNode(node *v1.Node) bool {
	return node != nil &&
		node.Labels["nvidia.com/gpu.deploy.dcgm-exporter"] == "true" &&
		node.Labels["nvidia.com/gpu.deploy.device-plugin"] == "true"
}