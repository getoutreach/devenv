package devenvutil

import (
	"encoding/json"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMarshalPods(t *testing.T) {
	podValues := pods{
		&corev1.Pod{
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
		&corev1.Pod{
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
		&corev1.Pod{
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
	tl := &testLog{ValueMap: map[string]interface{}{}}
	podValues.MarshalLog(tl.AddField)
	b, err := json.Marshal(tl.ValueMap)
	assert.NilError(t, err)
	t.Log(string(b))
	assert.Equal(t,
		// nolint:lll // long line
		`{"pod.pod1.containerstatuses.container1.ready":false,"pod.pod1.containerstatuses.container1.restart_count":0,"pod.pod1.containerstatuses.container1.state":"running","pod.pod1.phase":"Running","pod.pod1.status.message":"Running","pod.pod2.containerstatuses.container2.ready":false,"pod.pod2.containerstatuses.container2.restart_count":2,"pod.pod2.containerstatuses.container2.state":"terminated","pod.pod2.containerstatuses.container2.state.exit_code":1,"pod.pod2.containerstatuses.container2.state.reason":"crashed","pod.pod2.phase":"Failed","pod.pod3.containerstatuses.container1.ready":false,"pod.pod3.containerstatuses.container1.reason":"waiting on image","pod.pod3.containerstatuses.container1.restart_count":0,"pod.pod3.containerstatuses.container1.state":"waiting","pod.pod3.phase":"Failed"}`,
		string(b))
}

type testLog struct {
	ValueMap map[string]interface{}
}

func (t *testLog) AddField(key string, value interface{}) {
	t.ValueMap[key] = value
}
