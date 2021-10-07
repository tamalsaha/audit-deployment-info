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
	si.ProductInfo.LicenseID = "" // fix this
	si.ProductInfo.ProductOwnerName = info.ProductOwnerName
	si.ProductInfo.ProductOwnerUID = info.ProductOwnerUID
	si.ProductInfo.ProductName = info.ProductName
	si.ProductInfo.ProductUID = info.ProductUID
	si.ProductInfo.Version = Version{
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
	si.KubernetesInfo.ClusterName = clusterid.ClusterName()
	si.KubernetesInfo.ClusterUID, err = clusterid.ClusterUID(kc.CoreV1().Namespaces())
	if err != nil {
		return nil, err
	}
	si.KubernetesInfo.Version, err = kc.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}
	cert, err := meta_util.APIServerCertificate(cfg)
	if err != nil {
		return nil, err
	} else {
		si.KubernetesInfo.Certificate = &Certificate{
			SerialNumber: cert.SerialNumber.String(),
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
				host == "localhost"  ||
				!strings.ContainsRune(host, '.') {
				dnsNames.Delete(host)
			}
		}
		si.KubernetesInfo.Certificate.DNSNames = dnsNames.List()

		for _, ip := range cert.IPAddresses {
			if !skipIP(ip) {
				ips.Insert(ip.String())
			}
		}
		si.KubernetesInfo.Certificate.IPAddresses = ips.List()

		uris := make([]string, 0, len(cert.URIs))
		for _, u := range cert.URIs {
			uris = append(uris, u.String())
		}
		si.KubernetesInfo.Certificate.URIs = uris
	}

	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	si.KubernetesInfo.NodeStatus.Count = len(nodes)

	var capacity core.ResourceList
	var allocatable core.ResourceList
	for _, node := range nodes {
		capacity = api.AddResourceList(capacity, node.Status.Capacity)
		allocatable = api.AddResourceList(allocatable, node.Status.Allocatable)
	}
	si.KubernetesInfo.NodeStatus.Capacity = capacity
	si.KubernetesInfo.NodeStatus.Allocatable = allocatable

	return &si, nil
}

type SiteInfo struct {
	ProductInfo    ProductInfo    `json:"product_info"`
	KubernetesInfo KubernetesInfo `json:"kubernetes_info"`
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
	LicenseID string  `json:"license_id,omitempty"`

	ProductOwnerName string `json:"product_owner_name,omitempty"`
	ProductOwnerUID  string `json:"product_owner_uid,omitempty"`

	// This has been renamed to Features
	ProductName string `json:"product_name,omitempty"`
	ProductUID  string `json:"product_uid,omitempty"`
}

type KubernetesInfo struct {
	// https://github.com/kmodules/client-go/blob/master/tools/clusterid/lib.go
	ClusterName string        `json:"cluster_name,omitempty"`
	ClusterUID  string        `json:"cluster_uid,omitempty"`
	Version     *version.Info `json:"version,omitempty"`
	NodeStatus  NodeStatus    `json:"node_status"`
	Certificate *Certificate  `json:"certificate,omitempty"`
}

// https://github.com/kmodules/client-go/blob/kubernetes-1.16.3/tools/analytics/analytics.go#L66
type Certificate struct {
	SerialNumber string `json:"serial_number,omitempty"`
	//Issuer         pkix.Name  `json:"issuer"`
	//Subject        pkix.Name  `json:"subject"`
	NotBefore      metav1.Time `json:"not_before"`
	NotAfter       metav1.Time `json:"not_after"`
	DNSNames       []string    `json:"dns_names,omitempty"`
	EmailAddresses []string    `json:"email_addresses,omitempty"`
	IPAddresses    []string    `json:"ip_addresses,omitempty"`
	URIs           []string    `json:"ur_is,omitempty"`
}

type NodeStatus struct {
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
