package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cheggaaa/pb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type OrphanList struct {
	Pods      map[string]string
	Ingresses map[string]string
}

var inCluster bool

func betterPanic(message string, args ...string) {
	temp := fmt.Sprintf(message, args)
	fmt.Printf("%s\n\n", temp)
	os.Exit(1)
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func main() {
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	var kubeconfig *string
	home := homeDir()
	if home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Println("Local configuration not found, trying in-cluster configuration.")
		config, err = rest.InClusterConfig()
		if err != nil {
			betterPanic(err.Error())
		}
		inCluster = true
	}
	inCluster = false

	if inCluster {
		log.Printf("Configured to run in in-cluster mode.\n")
	} else {
		log.Printf("Configured to run in out-of cluster mode.\nService testing other than NodePort is not supported.")
	}

	log.Printf("Starting kube-cleanup.\n")

	clientset, err := kubernetes.NewForConfig(config)

	ingresses, err := clientset.ExtensionsV1beta1().Ingresses("").List(metav1.ListOptions{})
	if err != nil {
		betterPanic(err.Error())
	}

	orphans := make(map[string]OrphanList)

	fmt.Printf("Examining ingress rules.\n")
	bar := pb.StartNew(len(ingresses.Items))
	for _, ingress := range ingresses.Items {
		bar.Increment()

		for _, rule := range ingress.Spec.Rules {
			if rule.HTTP == nil {
				orphanList := orphans[ingress.Namespace]
				if orphanList.Ingresses == nil {
					orphanList.Ingresses = make(map[string]string)
				}
				orphanList.Ingresses[ingress.Name] = fmt.Sprintf("no HTTP routes in ingress")
				orphans[ingress.Namespace] = orphanList
				continue
			}
			for _, path := range rule.HTTP.Paths {

				serviceName := path.Backend.ServiceName
				_, err := clientset.CoreV1().Services(ingress.Namespace).Get(serviceName, metav1.GetOptions{})
				if err != nil {
					orphanList := orphans[ingress.Namespace]
					if orphanList.Ingresses == nil {
						orphanList.Ingresses = make(map[string]string)
					}
					orphanList.Ingresses[ingress.Name] = fmt.Sprintf("references a missing service: %s", serviceName)
					orphans[ingress.Namespace] = orphanList

					continue
				}
			}
		}
	}
	bar.Finish()

	pods, err := clientset.CoreV1().Pods("").List(metav1.ListOptions{})
	if err != nil {
		betterPanic(err.Error())
	}

	bar = pb.StartNew(len(pods.Items))

	fmt.Printf("Examining orphaned pods.\n")
	for _, pod := range pods.Items {
		bar.Increment()
		if "kube-system" == pod.Namespace {
			continue
		}
		//fmt.Printf(".\n")
		ownerReferences := pod.GetObjectMeta().GetOwnerReferences()
		for _, ownerReference := range ownerReferences {
			if "ReplicaSet" == ownerReference.Kind {
				rs, err := clientset.ExtensionsV1beta1().ReplicaSets(pod.Namespace).Get(ownerReference.Name, metav1.GetOptions{})
				if err != nil {
					orphanList := orphans[pod.Namespace]
					if orphanList.Pods == nil {
						orphanList.Pods = make(map[string]string)
					}
					orphanList.Pods[pod.Name] = fmt.Sprintf("owned by %s %s/%s, which is missing.n", ownerReference.Kind, pod.Namespace, ownerReference.Name)
					orphans[pod.Namespace] = orphanList

					continue
				} else {
					for _, ownerReference := range rs.OwnerReferences {
						if "Deployment" == ownerReference.Kind {
							_, err := clientset.ExtensionsV1beta1().Deployments(rs.Namespace).Get(ownerReference.Name, metav1.GetOptions{})
							if err != nil {
								orphanList := orphans[pod.Namespace]
								if orphanList.Pods == nil {
									orphanList.Pods = make(map[string]string)
								}
								orphanList.Pods[pod.Name] = fmt.Sprintf("deployment %s that owns the ReplicaSet %s, that owns the pod, is missing.\n", ownerReference.Name, rs.Name)
								orphans[pod.Namespace] = orphanList
							}
						}
					}
				}
			}
		}
		if len(ownerReferences) == 0 {
			orphanList := orphans[pod.Namespace]
			if orphanList.Pods == nil {
				orphanList.Pods = make(map[string]string)
			}
			orphanList.Pods[pod.Name] = fmt.Sprintf("pod %s/%s is not owned by anyone.\n", pod.Namespace, pod.Name)
			orphans[pod.Namespace] = orphanList
		}
	}
	bar.Finish()

	for namespace, orphanList := range orphans {
		fmt.Printf("\n==============================\n")
		fmt.Printf("Namespace: %s\n", namespace)
		fmt.Printf("==============================\n")
		if len(orphanList.Pods) > 0 {
			fmt.Printf("\nOrphaned Pods\n")
			for pod, reason := range orphanList.Pods {
				fmt.Printf("* %s, %s\n", pod, reason)
			}
		}
		if len(orphanList.Ingresses) > 0 {
			fmt.Printf("\nOrphaned Ingresses\n")
			for ingress, reason := range orphanList.Ingresses {
				fmt.Printf("* %s, %s\n", ingress, reason)
			}
		}
		fmt.Println()
	}
}
