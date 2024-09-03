package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type ResourceSummary struct {
	ResourceName string                       `json:"resourceName"`
	Group        string                       `json:"group"`
	Version      string                       `json:"version"`
	Names        []string                     `json:"names"`
	Labels       map[string]map[string]string `json:"labels"` // Add labels field
}

func main() {
	var port string
	flag.StringVar(&port, "port", "8080", "Server port")
	flag.Parse()

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeconfig.ClientConfig()
	if err != nil {
		log.Fatalf("Error getting Kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes clientset: %v", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating dynamic client: %v", err)
	}

	http.HandleFunc("/resources", func(w http.ResponseWriter, r *http.Request) {
		handleResources(w, r, clientset, dynClient)
	})

	log.Printf("Starting server on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Error starting HTTP server: %v", err)
	}
}

func handleResources(w http.ResponseWriter, r *http.Request, clientset *kubernetes.Clientset, dynClient dynamic.Interface) {
	discoveryClient := clientset.Discovery()
	serverResources, err := discoveryClient.ServerPreferredResources()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error retrieving server resources: %v", err), http.StatusInternalServerError)
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	summaries := make([]ResourceSummary, 0)

	for _, group := range serverResources {
		for _, resource := range group.APIResources {
			if isSubResource(resource.Name) {
				continue
			}

			wg.Add(1)
			go func(resource metav1.APIResource, groupVersion string) {
				defer wg.Done()

				gvr := schema.GroupVersionResource{
					Group:    strings.Split(groupVersion, "/")[0],
					Version:  resource.Version,
					Resource: resource.Name,
				}
				if gvr.Group == "v1" {
					gvr.Version = gvr.Group
					gvr.Group = ""
				}

				list, err := dynClient.Resource(gvr).List(context.TODO(), metav1.ListOptions{})
				if err != nil {
					log.Printf("Error listing %s: %v", resource.Name, err)
					return
				}

				resourceNames := make([]string, 0, len(list.Items))
				resourceLabels := make(map[string]map[string]string) // Map to store labels for each resource name

				for _, item := range list.Items {
					resourceNames = append(resourceNames, item.GetName())
					resourceLabels[item.GetName()] = item.GetLabels() // Store the labels
				}

				mu.Lock()
				summaries = append(summaries, ResourceSummary{
					ResourceName: resource.Name,
					Group:        gvr.Group,
					Version:      gvr.Version,
					Names:        resourceNames,
					Labels:       resourceLabels, // Add labels to summary
				})
				mu.Unlock()
			}(resource, group.GroupVersion)
		}
	}

	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summaries); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func isSubResource(name string) bool {
	return strings.Contains(name, "/")
}
