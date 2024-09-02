package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type ResourceSummary struct {
	ResourceName string
	Group        string
	Version      string
	Names        []string
}

func main() {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeconfig.ClientConfig()
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	http.HandleFunc("/resources", func(w http.ResponseWriter, r *http.Request) {
		discoveryClient := clientset.Discovery()

		// Get the list of all API resources available
		serverResources, err := discoveryClient.ServerPreferredResources()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var summaries []ResourceSummary

		for _, group := range serverResources {
			for _, resource := range group.APIResources {
				// Skip subresources like pod/logs, pod/status
				if containsSlash(resource.Name) {
					continue
				}

				gvr := schema.GroupVersionResource{
					Group:    strings.Split(group.GroupVersion, "/")[0],
					Version:  resource.Version,
					Resource: resource.Name,
				}
				if gvr.Group == "v1" {
					gvr.Version = gvr.Group
					gvr.Group = ""
				}

				var list *unstructured.UnstructuredList
				if resource.Namespaced {
					list, err = dynClient.Resource(gvr).List(context.TODO(), metav1.ListOptions{})
				} else {
					list, err = dynClient.Resource(gvr).List(context.TODO(), metav1.ListOptions{})
				}

				if err != nil {
					fmt.Printf("..Error listing %s: %v. Group %q Version %q Resource %q\n", resource.Name, err,
						gvr.Group, gvr.Version, gvr.Resource)
					continue
				}

				resourceNames := make([]string, 0, len(list.Items))
				for _, item := range list.Items {
					resourceNames = append(resourceNames, item.GetName())
				}

				summary := ResourceSummary{
					ResourceName: resource.Name,
					Group:        gvr.Group,
					Version:      gvr.Version,
					Names:        resourceNames,
				}
				summaries = append(summaries, summary)
			}
		}

		renderHTML(w, summaries)
	})

	fmt.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

func renderHTML(w http.ResponseWriter, summaries []ResourceSummary) {
	// Define the HTML template
	const tpl = `
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Kubernetes Resources</title>
		<style>
			body { font-family: Arial, sans-serif; }
			table { width: 100%; border-collapse: collapse; }
			th, td { padding: 8px 12px; border: 1px solid #ddd; text-align: left; }
			th { background-color: #f4f4f4; }
		</style>
	</head>
	<body>
		<h1>Kubernetes Resources</h1>
		<table>
			<thead>
				<tr>
					<th>Resource Type</th>
					<th>Group</th>
					<th>Version</th>
					<th>Names</th>
				</tr>
			</thead>
			<tbody>
				{{range .}}
				<tr>
					<td>{{.ResourceName}}</td>
					<td>{{.Group}}</td>
					<td>{{.Version}}</td>
					<td>{{if .Names}}{{range .Names}}<div>{{.}}</div>{{end}}{{else}}<em>No resources found</em>{{end}}</td>
				</tr>
				{{end}}
			</tbody>
		</table>
	</body>
	</html>`

	// Parse and execute the template
	tmpl := template.Must(template.New("resources").Parse(tpl))
	if err := tmpl.Execute(w, summaries); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func containsSlash(s string) bool {
	return len(s) > 0 && s[0] == '/'
}
