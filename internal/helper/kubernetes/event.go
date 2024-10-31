package kubernetes

import (
	"github.com/gophercloud/gophercloud/v2"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

// SendErrorAsEvent sends an event for an error to the given objects and returns the error.
// It cast some openstack errors to send more information of the error in the event.
func SendErrorAsEvent(r record.EventRecorder, err error, objects ...runtime.Object) error {
	if err == nil {
		return nil
	}
	var body []byte
	switch casted := err.(type) {
	case gophercloud.ErrUnexpectedResponseCode:
		body = casted.Body
	default:
		body = []byte(err.Error())
	}

	if len(body) == 0 {
		return err
	}

	for _, ob := range objects {
		r.Event(ob, coreV1.EventTypeWarning, "Failed", string(body))
	}
	return err
}
