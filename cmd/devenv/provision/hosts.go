package provision

import (
	"context"
	"time"

	"github.com/getoutreach/gobox/pkg/async"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ingressControllerIPAnnotation = "devenv.outreach.io/local-ip"

// getIngressControllerIP finds the IP address of the ingress controller
// being used in the devenv
func (o *Options) getIngressControllerIP(ctx context.Context) string {
	fallbackIP := "127.0.0.1"

	if o.k != nil {
		s, err := o.k.CoreV1().Services("nginx-ingress").Get(ctx, "ingress-nginx-controller", metav1.GetOptions{})
		if err != nil {
			return fallbackIP
		}

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

		// iterate over the ingress to find its IP, if it doesn't
		// have one then we should wait until it gets one
		for ctx.Err() == nil {
			s, err = o.k.CoreV1().Services("nginx-ingress").Get(ctx, "ingress-nginx-controller", metav1.GetOptions{})
			if err == nil {
				for i := range s.Status.LoadBalancer.Ingress {
					ing := &s.Status.LoadBalancer.Ingress[i]
					return ing.IP
				}
			}

			o.log.Info("Waiting for ingress controller to get an IP")
			async.Sleep(ctx, time.Second*10)
		}
	}

	return fallbackIP
}
