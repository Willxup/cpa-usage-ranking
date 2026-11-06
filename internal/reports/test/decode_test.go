package test

import (
	"errors"
	"strings"
	"testing"

	"cpa-usage-ranking/internal/models"
	"cpa-usage-ranking/internal/reports"
)

const validPayload = `{"schema_version":1,"revision":"8","items":[{"model":"vendor/model","total_tokens":"9223372036854775807","input_tokens":"10","output_tokens":"5","cache_read_tokens":"1","cache_write_tokens":"2","reasoning_tokens":"3","request_count":"2","success_count":"1","ttft_avg_ms":null,"ttft_samples":"0","latency_avg_ms":0,"latency_samples":"2"}]}`

func TestDecodeVersionedReport(t *testing.T) {
	report, err := reports.Decode([]byte(validPayload))
	if err != nil || report.Revision != 8 || len(report.Items) != 1 {
		t.Fatalf("Decode: %+v %v", report, err)
	}
	item := report.Items[0]
	if item.Model != "vendor/model" || item.Tier != models.Default || item.TotalTokens != 9223372036854775807 || item.InputTokens != 10 || item.OutputTokens != 5 || item.CacheReadTokens != 1 || item.CacheWriteTokens != 2 || item.ReasoningTokens != 3 || item.RequestCount != 2 || item.SuccessCount != 1 || item.TTFTAvgMS != nil || item.TTFTSamples != 0 || item.LatencyAvgMS == nil || *item.LatencyAvgMS != 0 || item.LatencySamples != 2 {
		t.Fatalf("decoded item = %+v", item)
	}
	for _, tc := range []struct {
		tier string
		want models.Tier
	}{{"default", models.Default}, {"priority", models.Priority}, {"flex", models.Flex}} {
		payload := strings.Replace(validPayload, `"model":"vendor/model"`, `"model":"vendor/model","service_tier":"`+tc.tier+`"`, 1)
		got, err := reports.Decode([]byte(payload))
		if err != nil || got.Items[0].Tier != tc.want {
			t.Fatalf("tier %s: %+v %v", tc.tier, got, err)
		}
	}
	for _, payload := range []string{`{"schema_version":1,"revision":"0","items":[]}`, strings.Replace(validPayload, `"revision":"8"`, `"revision":"0008"`, 1), strings.Replace(validPayload, `"model":"vendor/model"`, `"model":"\ud83d\ude00"`, 1)} {
		if _, err := reports.Decode([]byte(payload)); err != nil {
			t.Fatalf("valid payload rejected: %v", err)
		}
	}
}

func TestDecodeRejectsMissingNullIncorrectTypesAndDuplicates(t *testing.T) {
	cases := map[string]string{
		"missing version":   strings.Replace(validPayload, `"schema_version":1,`, "", 1),
		"null version":      strings.Replace(validPayload, `"schema_version":1`, `"schema_version":null`, 1),
		"string version":    strings.Replace(validPayload, `"schema_version":1`, `"schema_version":"1"`, 1),
		"future version":    strings.Replace(validPayload, `"schema_version":1`, `"schema_version":2`, 1),
		"missing revision":  strings.Replace(validPayload, `"revision":"8",`, "", 1),
		"null revision":     strings.Replace(validPayload, `"revision":"8"`, `"revision":null`, 1),
		"numeric revision":  strings.Replace(validPayload, `"revision":"8"`, `"revision":8`, 1),
		"negative revision": strings.Replace(validPayload, `"revision":"8"`, `"revision":"-1"`, 1),
		"revision overflow": strings.Replace(validPayload, `"revision":"8"`, `"revision":"9223372036854775808"`, 1),
		"plus revision":     strings.Replace(validPayload, `"revision":"8"`, `"revision":"+1"`, 1),
		"missing items":     `{"schema_version":1,"revision":"0"}`,
		"null items":        `{"schema_version":1,"revision":"0","items":null}`,
		"object items":      `{"schema_version":1,"revision":"0","items":{}}`,
		"null item":         `{"schema_version":1,"revision":"0","items":[null]}`,
		"missing model":     strings.Replace(validPayload, `"model":"vendor/model",`, "", 1),
		"null model":        strings.Replace(validPayload, `"model":"vendor/model"`, `"model":null`, 1),
		"invalid surrogate": strings.Replace(validPayload, `"model":"vendor/model"`, `"model":"\ud800"`, 1),
		"null tier":         strings.Replace(validPayload, `"model":"vendor/model"`, `"model":"vendor/model","service_tier":null`, 1),
		"empty tier":        strings.Replace(validPayload, `"model":"vendor/model"`, `"model":"vendor/model","service_tier":""`, 1),
		"wrong tier":        strings.Replace(validPayload, `"model":"vendor/model"`, `"model":"vendor/model","service_tier":1`, 1),
		"unknown tier":      strings.Replace(validPayload, `"model":"vendor/model"`, `"model":"vendor/model","service_tier":"prority"`, 1),
		"missing tokens":    strings.Replace(validPayload, `"input_tokens":"10",`, "", 1),
		"null tokens":       strings.Replace(validPayload, `"input_tokens":"10"`, `"input_tokens":null`, 1),
		"numeric tokens":    strings.Replace(validPayload, `"input_tokens":"10"`, `"input_tokens":10`, 1),
		"fraction tokens":   strings.Replace(validPayload, `"input_tokens":"10"`, `"input_tokens":"1.0"`, 1),
		"token overflow":    strings.Replace(validPayload, `"input_tokens":"10"`, `"input_tokens":"9223372036854775808"`, 1),
		"missing avg":       strings.Replace(validPayload, `"ttft_avg_ms":null,`, "", 1),
		"string avg":        strings.Replace(validPayload, `"ttft_avg_ms":null`, `"ttft_avg_ms":"0"`, 1),
		"boolean avg":       strings.Replace(validPayload, `"ttft_avg_ms":null`, `"ttft_avg_ms":false`, 1),
		"nonfinite":         strings.Replace(validPayload, `"ttft_avg_ms":null`, `"ttft_avg_ms":1e999`, 1),
		"duplicate key":     strings.Replace(validPayload, `"revision":"8"`, `"revision":"8","revision":"9"`, 1),
		"unknown key":       strings.Replace(validPayload, `"revision":"8"`, `"revision":"8","client_id":"1"`, 1),
		"trailing object":   validPayload + `{}`,
		"invalid utf8":      strings.Replace(validPayload, "vendor/model", string([]byte{0xff}), 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := reports.Decode([]byte(input)); !errors.Is(err, reports.ErrInvalid) {
				t.Fatalf("expected invalid report, got %v", err)
			}
		})
	}
}
