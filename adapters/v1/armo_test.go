package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/armosec/armoapi-go/armotypes"
	v1 "github.com/armosec/cluster-container-scanner-api/containerscan/v1"
	"github.com/armosec/utils-go/httputils"
	"github.com/armosec/utils-k8s-go/armometadata"
	"github.com/go-test/deep"
	"github.com/google/uuid"
	"github.com/kubescape/kubevuln/core/domain"
)

func TestArmoAdapter_GetCVEExceptions(t *testing.T) {
	type fields struct {
		clusterConfig        armometadata.ClusterConfig
		getCVEExceptionsFunc func(string, string, *armotypes.PortalDesignator) ([]armotypes.VulnerabilityExceptionPolicy, error)
	}
	tests := []struct {
		name     string
		workload bool
		fields   fields
		want     domain.CVEExceptions
		wantErr  bool
	}{
		{
			name:     "no workload",
			workload: false,
			wantErr:  true,
		},
		{
			name:     "error get exceptions",
			workload: true,
			fields: fields{
				getCVEExceptionsFunc: func(s string, s2 string, designator *armotypes.PortalDesignator) ([]armotypes.VulnerabilityExceptionPolicy, error) {
					return nil, errors.New("error")
				},
			},
			wantErr: true,
		},
		{
			name:     "no exception",
			workload: true,
			fields: fields{
				getCVEExceptionsFunc: func(s string, s2 string, designator *armotypes.PortalDesignator) ([]armotypes.VulnerabilityExceptionPolicy, error) {
					return []armotypes.VulnerabilityExceptionPolicy{}, nil
				},
			},
			want: []armotypes.VulnerabilityExceptionPolicy{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &ArmoAdapter{
				clusterConfig:        tt.fields.clusterConfig,
				getCVEExceptionsFunc: tt.fields.getCVEExceptionsFunc,
			}
			ctx := context.TODO()
			if tt.workload {
				ctx = context.WithValue(ctx, domain.WorkloadKey{}, domain.ScanCommand{})
			}
			got, err := a.GetCVEExceptions(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCVEExceptions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			diff := deep.Equal(got, tt.want)
			if diff != nil {
				t.Errorf("compare failed: %v", diff)
			}
		})
	}
}

func fileToCVEManifest(path string) domain.CVEManifest {
	var cve domain.CVEManifest
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(b, &cve)
	if err != nil {
		panic(err)
	}
	return cve
}

func TestArmoAdapter_SubmitCVE(t *testing.T) {
	getCVEExceptionsFunc := func(s string, s2 string, designator *armotypes.PortalDesignator) ([]armotypes.VulnerabilityExceptionPolicy, error) {
		return []armotypes.VulnerabilityExceptionPolicy{}, nil
	}
	tests := []struct {
		name    string
		cve     domain.CVEManifest
		cvep    domain.CVEManifest
		wantErr bool
	}{
		{
			name: "submit small cve",
			cve:  fileToCVEManifest("testdata/nginx-cve-small.json"),
			cvep: domain.CVEManifest{},
		},
		{
			name: "submit big cve",
			cve:  fileToCVEManifest("testdata/nginx-cve.json"),
			cvep: domain.CVEManifest{},
		},
		{
			name: "submit big cve with relevancy",
			cve:  fileToCVEManifest("testdata/nginx-cve.json"),
			cvep: fileToCVEManifest("testdata/nginx-filtered-cve.json"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mu := &sync.Mutex{}
			seenCVE := map[string]struct{}{}
			httpPostFunc := func(httpClient httputils.IHttpClient, fullURL string, headers map[string]string, body []byte) (*http.Response, error) {
				var report v1.ScanResultReport
				err := json.Unmarshal(body, &report)
				if err != nil {
					t.Errorf("failed to unmarshal report: %v", err)
				}
				mu.Lock()
				for _, v := range report.Vulnerabilities {
					id := v.Name + "+" + v.RelatedPackageName
					if _, ok := seenCVE[id]; ok {
						t.Errorf("duplicate cve %s", id)
					}
					seenCVE[id] = struct{}{}
				}
				mu.Unlock()
				return &http.Response{
					StatusCode: 200,
					Body:       io.NopCloser(bytes.NewBuffer([]byte{})),
				}, nil
			}
			a := &ArmoAdapter{
				clusterConfig:        armometadata.ClusterConfig{},
				getCVEExceptionsFunc: getCVEExceptionsFunc,
				httpPostFunc:         httpPostFunc,
			}
			ctx := context.TODO()
			ctx = context.WithValue(ctx, domain.TimestampKey{}, time.Now().Unix())
			ctx = context.WithValue(ctx, domain.ScanIDKey{}, uuid.New().String())
			ctx = context.WithValue(ctx, domain.WorkloadKey{}, domain.ScanCommand{})
			if err := a.SubmitCVE(ctx, tt.cve, tt.cvep); (err != nil) != tt.wantErr {
				t.Errorf("SubmitCVE() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewArmoAdapter(t *testing.T) {
	type args struct {
		accountID            string
		gatewayRestURL       string
		eventReceiverRestURL string
	}
	tests := []struct {
		name string
		args args
		want *ArmoAdapter
	}{
		{
			name: "new armo adapter",
			want: &ArmoAdapter{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewArmoAdapter(tt.args.accountID, tt.args.gatewayRestURL, tt.args.eventReceiverRestURL)
			// need to nil functions to compare
			got.httpPostFunc = nil
			got.getCVEExceptionsFunc = nil
			diff := deep.Equal(got, tt.want)
			if diff != nil {
				t.Errorf("compare failed: %v", diff)
			}
		})
	}
}
