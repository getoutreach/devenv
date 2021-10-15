package devenvutil

import (
	"context"
	"time"

	"github.com/getoutreach/gobox/pkg/async"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ingressControllerIPAnnotation = "devenv.outreach.io/local-ip"

// GetIngressControllerIP finds the IP address of the ingress controller
// being used in the devenv
func GetIngressControllerIP(ctx context.Context, k kubernetes.Interface, log logrus.FieldLogger) string {
	fallbackIP := "127.0.0.1"

	if k != nil {
		// iterate over the ingress to find its IP, if it doesn't
		// have one then we should wait until it gets one
		for ctx.Err() == nil {
			s, err := k.CoreV1().Services("nginx-ingress").Get(ctx, "ingress-nginx-controller", metav1.GetOptions{})
			if err == nil {
				// return the value of the ingress controller IP annotation if
				// it's found.
				if _, ok := s.Annotations[ingressControllerIPAnnotation]; ok {
					return s.Annotations[ingressControllerIPAnnotation]
				}

				// if we're not a type loadbalancer, return the fallback IP
				// we have no idea where it is accessible
				if s.Spec.Type != corev1.ServiceTypeLoadBalancer {
					return fallbackIP
				}

				for i := range s.Status.LoadBalancer.Ingress {
					ing := &s.Status.LoadBalancer.Ingress[i]
					return ing.IP
				}
			}

			log.Info("Waiting for ingress controller to get an IP")
			async.Sleep(ctx, time.Second*10)
		}
	}

	return fallbackIP
}
