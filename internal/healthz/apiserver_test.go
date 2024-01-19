package healthz_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	fakerestclient "k8s.io/client-go/rest/fake"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	. "github.com/stackitcloud/yawol/internal/healthz"
)

var _ = Describe("NewAPIServerHealthz", func() {
	var (
		ctx            = context.TODO()
		fakeRESTClient *fakerestclient.RESTClient
		checker        healthz.Checker
	)

	BeforeEach(func() {
		fakeRESTClient = &fakerestclient.RESTClient{
			NegotiatedSerializer: serializer.NewCodecFactory(scheme.Scheme).WithoutConversion(),
			Resp: &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("")),
			},
		}
		checker = NewAPIServerHealthz(ctx, fakeRESTClient)
	})

	It("should return no error because the request succeeded", func() {
		Expect(checker(nil)).To(Succeed())
	})

	It("should return an error because the request failed", func() {
		fakeErr := fmt.Errorf("fake err")
		fakeRESTClient.Err = fakeErr

		Expect(checker(nil)).To(MatchError(fakeErr))
	})

	It("should return an error because the response was not 200 OK", func() {
		fakeRESTClient.Resp.StatusCode = http.StatusAccepted

		Expect(checker(nil)).To(MatchError(ContainSubstring("failed talking to the cluster's kube-apiserver")))
	})
})
