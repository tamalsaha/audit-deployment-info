package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"

	"go.bytebuilders.dev/license-verifier/info"
	v "gomodules.xyz/x/version"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/version"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
	meta_util "kmodules.xyz/client-go/meta"
	"kmodules.xyz/client-go/tools/clusterid"
	"kmodules.xyz/resource-metrics/api"
)

func main() {
	masterURL := ""
	kubeconfigPath := filepath.Join(homedir.HomeDir(), ".kube", "config")
	kubeconfigPath = "/home/tamal/Downloads/mysql-test-kubeconfig.yaml"

	config, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfigPath)
	if err != nil {
		log.Fatalf("Could not get Kubernetes config: %s", err)
	}

	kc := kubernetes.NewForConfigOrDie(config)

	factory := informers.NewSharedInformerFactory(kc, 0)
	nodeInformer := factory.Core().V1().Nodes().Informer()
	nodeLister := factory.Core().V1().Nodes().Lister()
	nodeInformer.AddEventHandlerWithResyncPeriod(&ResourceEventPublisher{}, 0) // c.Auditor.ForGVK(api.SchemeGroupVersion.WithKind(api.ResourceKindPostgres)))

	stopCh := genericapiserver.SetupSignalHandler()
	factory.Start(stopCh)
	// Wait for all involved caches to be synced, before processing items from the queue is started
	for t, ok := range factory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Fatalf("%v timed out waiting for caches to sync", t)
			return
		}
	}

	si, err := GenerateSiteInfo(config, kc, nodeLister)
	if err != nil {
		panic(err)
	}
	data, err := json.MarshalIndent(si, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
}

func GenerateSiteInfo(cfg *rest.Config, kc kubernetes.Interface, nodeLister v1.NodeLister) (*SiteInfo, error) {
	var si SiteInfo
	si.Product.LicenseID = "" // fix this
	si.Product.ProductOwnerName = info.ProductOwnerName
	si.Product.ProductOwnerUID = info.ProductOwnerUID
	si.Product.ProductName = info.ProductName
	si.Product.ProductUID = info.ProductUID
	si.Product.Version = Version{
		Version:         v.Version.Version,
		VersionStrategy: v.Version.VersionStrategy,
		CommitHash:      v.Version.CommitHash,
		GitBranch:       v.Version.GitBranch,
		GitTag:          v.Version.GitTag,
		CommitTimestamp: v.Version.CommitTimestamp,
		GoVersion:       v.Version.GoVersion,
		Compiler:        v.Version.Compiler,
		Platform:        v.Version.Platform,
	}

	var err error
	si.Kubernetes.ClusterName = clusterid.ClusterName()
	si.Kubernetes.ClusterUID, err = clusterid.ClusterUID(kc.CoreV1().Namespaces())
	if err != nil {
		return nil, err
	}
	si.Kubernetes.Version, err = kc.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}
	cert, err := meta_util.APIServerCertificate(cfg)
	if err != nil {
		return nil, err
	} else {
		si.Kubernetes.ControlPlane = &ControlPlaneInfo{
			//SerialNumber: cert.SerialNumber.String(),
			//Issuer:         cert.Issuer,
			//Subject:        cert.Subject,
			NotBefore: metav1.NewTime(cert.NotBefore),
			NotAfter:  metav1.NewTime(cert.NotAfter),
			// DNSNames:       cert.DNSNames,
			EmailAddresses: cert.EmailAddresses,
			// IPAddresses:    cert.IPAddresses,
			// URIs:           cert.URIs,
		}

		dnsNames := sets.NewString(cert.DNSNames...)
		ips := sets.NewString()
		if len(cert.Subject.CommonName) > 0 {
			if ip := net.ParseIP(cert.Subject.CommonName); ip != nil {
				if !skipIP(ip) {
					ips.Insert(ip.String())
				}
			} else {
				dnsNames.Insert(cert.Subject.CommonName)
			}
		}

		for _, host := range dnsNames.UnsortedList() {
			if host == "kubernetes" ||
				host == "kubernetes.default" ||
				host == "kubernetes.default.svc" ||
				strings.HasSuffix(host, ".svc.cluster.local") ||
				host == "localhost" ||
				!strings.ContainsRune(host, '.') {
				dnsNames.Delete(host)
			}
		}
		si.Kubernetes.ControlPlane.DNSNames = dnsNames.List()

		for _, ip := range cert.IPAddresses {
			if !skipIP(ip) {
				ips.Insert(ip.String())
			}
		}
		si.Kubernetes.ControlPlane.IPAddresses = ips.List()

		uris := make([]string, 0, len(cert.URIs))
		for _, u := range cert.URIs {
			uris = append(uris, u.String())
		}
		si.Kubernetes.ControlPlane.URIs = uris
	}

	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	si.Kubernetes.NodeStats.Count = len(nodes)

	var capacity core.ResourceList
	var allocatable core.ResourceList
	for _, node := range nodes {
		capacity = api.AddResourceList(capacity, node.Status.Capacity)
		allocatable = api.AddResourceList(allocatable, node.Status.Allocatable)
	}
	si.Kubernetes.NodeStats.Capacity = capacity
	si.Kubernetes.NodeStats.Allocatable = allocatable

	return &si, nil
}

type SiteInfo struct {
	Product    ProductInfo    `json:"product"`
	Kubernetes KubernetesInfo `json:"kubernetes"`
}

type Version struct {
	Version         string `json:"version,omitempty"`
	VersionStrategy string `json:"versionStrategy,omitempty"`
	CommitHash      string `json:"commitHash,omitempty"`
	GitBranch       string `json:"gitBranch,omitempty"`
	GitTag          string `json:"gitTag,omitempty"`
	CommitTimestamp string `json:"commitTimestamp,omitempty"`
	GoVersion       string `json:"goVersion,omitempty"`
	Compiler        string `json:"compiler,omitempty"`
	Platform        string `json:"platform,omitempty"`
}

type ProductInfo struct {
	Version   Version `json:"version"`
	LicenseID string  `json:"licenseID,omitempty"`

	ProductOwnerName string `json:"productOwnerName,omitempty"`
	ProductOwnerUID  string `json:"productOwnerUID,omitempty"`

	// This has been renamed to Features
	ProductName string `json:"productName,omitempty"`
	ProductUID  string `json:"productUID,omitempty"`
}

type KubernetesInfo struct {
	// https://github.com/kmodules/client-go/blob/master/tools/clusterid/lib.go
	ClusterName  string            `json:"clusterName,omitempty"`
	ClusterUID   string            `json:"clusterUID,omitempty"`
	Version      *version.Info     `json:"version,omitempty"`
	ControlPlane *ControlPlaneInfo `json:"controlPlane,omitempty"`
	NodeStats    NodeStats         `json:"nodeStats"`
}

// https://github.com/kmodules/client-go/blob/kubernetes-1.16.3/tools/analytics/analytics.go#L66
type ControlPlaneInfo struct {
	DNSNames       []string    `json:"dnsNames,omitempty"`
	EmailAddresses []string    `json:"emailAddresses,omitempty"`
	IPAddresses    []string    `json:"ipAddresses,omitempty"`
	URIs           []string    `json:"uris,omitempty"`
	NotBefore      metav1.Time `json:"notBefore"`
	NotAfter       metav1.Time `json:"notAfter"`
}

type NodeStats struct {
	Count int `json:"count,omitempty"`

	// Capacity represents the total resources of a node.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#capacity
	// +optional
	Capacity core.ResourceList `json:"capacity,omitempty"`

	// Allocatable represents the resources of a node that are available for scheduling.
	// Defaults to Capacity.
	// +optional
	Allocatable core.ResourceList `json:"allocatable,omitempty"`
}

func skipIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsMulticast() ||
		ip.IsGlobalUnicast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsLinkLocalUnicast()
}

var _ cache.ResourceEventHandler = &ResourceEventPublisher{}

type ResourceEventPublisher struct {
}

func (p ResourceEventPublisher) OnAdd(obj interface{}) {

}

func (p ResourceEventPublisher) OnUpdate(oldObj, newObj interface{}) {

}

func (p ResourceEventPublisher) OnDelete(obj interface{}) {

}
