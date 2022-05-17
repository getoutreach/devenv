package devenvutil

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMarshalPods(t *testing.T) {
	podValues := []*corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod1"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "container1", State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{},
						},
					},
				},
				Phase:   corev1.PodRunning,
				Message: "Running",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod2"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						RestartCount: 2,
						Name:         "container2", State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 1,
								Reason:   "crashed",
							},
						},
					},
				},
				Phase: corev1.PodFailed,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "pod3"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "container1", State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason:  "waiting on image",
								Message: "Waiting for image message ",
							},
						},
					},
				},
				Phase: corev1.PodFailed,
			},
		},
	}

	// Writing this out here as a test to show how it actually writes
	buf := &bytes.Buffer{}
	log := logrus.New()
	log.SetOutput(buf)

	log.WithField("pods", PodsStateInfo(podValues)).Info("Test")

	assert.Assert(t,
		// nolint:lll // long line
		strings.Contains(buf.String(), `pods="map[pod1:map[Message:Running Phase:Running container1:map[Ready:false Restart:0 State:running]] pod2:map[Phase:Failed container2:map[ExitCode:1 Ready:false Reason:crashed Restart:2 State:terminated]] pod3:map[Phase:Failed container1:map[Ready:false Reason:waiting on image Restart:0 State:waiting]]]`))
}
