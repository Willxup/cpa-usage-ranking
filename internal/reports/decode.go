// Package reports implements the daily snapshot protocol independently of HTTP.
package reports

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"

	"cpa-usage-ranking/internal/models"
)

var ErrInvalid = errors.New("invalid report")
var ErrTooManyItems = errors.New("too many report items")

// Report contains the typed input, before validation or catalog matching.
type Report struct {
	Revision int64
	Items    []Item
}

type Item struct {
	Model            string
	Tier             models.Tier
	TotalTokens      int64
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	ReasoningTokens  int64
	RequestCount     int64
	SuccessCount     int64
	TTFTAvgMS        *float64
	TTFTSamples      int64
	LatencyAvgMS     *float64
	LatencySamples   int64
}

// Decode accepts exactly one schema-version-1 report. Semantic metric and
// model-name validation is separate. Errors never contain submitted values.
func Decode(data []byte) (Report, error) {
	return decode(data, 0)
}

// DecodeLimited bounds items before individual item decoding. The limit applies
// to raw entries, not post-catalog model/tier combinations.
func DecodeLimited(data []byte, maxItems int) (Report, error) {
	if maxItems <= 0 {
		return Report{}, ErrInvalid
	}
	return decode(data, maxItems)
}

func decode(data []byte, maxItems int) (Report, error) {
	if !utf8.Valid(data) {
		return Report{}, ErrInvalid
	}
	obj, err := object(data, "schema_version", "revision", "items")
	if err != nil {
		return Report{}, ErrInvalid
	}
	var version int
	if raw, ok := obj["schema_version"]; !ok || json.Unmarshal(raw, &version) != nil || version != 1 {
		return Report{}, ErrInvalid
	}
	revision, err := integer(obj["revision"])
	if err != nil || revision < 0 {
		return Report{}, ErrInvalid
	}
	rawItems, ok := obj["items"]
	if !ok || isNull(rawItems) {
		return Report{}, ErrInvalid
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawItems, &entries); err != nil || entries == nil {
		return Report{}, ErrInvalid
	}
	if maxItems > 0 && len(entries) > maxItems {
		return Report{}, ErrTooManyItems
	}
	report := Report{Revision: revision, Items: make([]Item, 0, len(entries))}
	for _, entry := range entries {
		item, err := decodeItem(entry)
		if err != nil {
			return Report{}, ErrInvalid
		}
		report.Items = append(report.Items, item)
	}
	return report, nil
}

func decodeItem(data []byte) (Item, error) {
	obj, err := object(data, "model", "service_tier", "total_tokens", "input_tokens", "output_tokens",
		"cache_read_tokens", "cache_write_tokens", "reasoning_tokens", "request_count", "success_count",
		"ttft_avg_ms", "ttft_samples", "latency_avg_ms", "latency_samples")
	if err != nil {
		return Item{}, err
	}
	var item Item
	if raw, ok := obj["model"]; !ok || isNull(raw) || json.Unmarshal(raw, &item.Model) != nil || !validUnicodeEscapes(raw) {
		return Item{}, ErrInvalid
	}
	var tier *string
	rawTier, present := obj["service_tier"]
	if present && !isNull(rawTier) {
		var text string
		if json.Unmarshal(rawTier, &text) != nil {
			return Item{}, ErrInvalid
		}
		tier = &text
	}
	item.Tier, err = models.ReportedTier(present, tier)
	if err != nil {
		return Item{}, ErrInvalid
	}
	for _, field := range []struct {
		name  string
		value *int64
	}{
		{"total_tokens", &item.TotalTokens}, {"input_tokens", &item.InputTokens},
		{"output_tokens", &item.OutputTokens}, {"cache_read_tokens", &item.CacheReadTokens},
		{"cache_write_tokens", &item.CacheWriteTokens}, {"reasoning_tokens", &item.ReasoningTokens},
		{"request_count", &item.RequestCount}, {"success_count", &item.SuccessCount},
		{"ttft_samples", &item.TTFTSamples}, {"latency_samples", &item.LatencySamples},
	} {
		*field.value, err = integer(obj[field.name])
		if err != nil {
			return Item{}, ErrInvalid
		}
	}
	for _, field := range []struct {
		name  string
		value **float64
	}{{"ttft_avg_ms", &item.TTFTAvgMS}, {"latency_avg_ms", &item.LatencyAvgMS}} {
		raw, ok := obj[field.name]
		if !ok {
			return Item{}, ErrInvalid
		}
		if !isNull(raw) {
			var value float64
			if err := json.Unmarshal(raw, &value); err != nil {
				return Item{}, ErrInvalid
			}
			*field.value = &value
		}
	}
	return item, nil
}

// integer accepts decimal strings with an optional minus and leading zeroes;
// callers enforce the protocol's nonnegative semantic constraints.
func integer(raw json.RawMessage) (int64, error) {
	var text string
	if len(raw) == 0 || isNull(raw) || json.Unmarshal(raw, &text) != nil || len(text) == 0 {
		return 0, ErrInvalid
	}
	for i, c := range text {
		if c < '0' || c > '9' {
			if i != 0 || c != '-' || len(text) == 1 {
				return 0, ErrInvalid
			}
		}
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	return value, nil
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// encoding/json replaces unpaired surrogate escapes; reject them instead.
func validUnicodeEscapes(raw []byte) bool {
	for i := 1; i+1 < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		code, _ := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16) // JSON was parsed above.
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return false
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}

// object also rejects duplicate and unknown keys, instead of silently letting
// the last occurrence change the meaning of a signed snapshot.
func object(data []byte, allowed ...string) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalid
	}
	keys := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		keys[key] = true
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for dec.More() {
		keyToken, err := dec.Token()
		key, ok := keyToken.(string)
		if err != nil || !ok || !keys[key] {
			return nil, ErrInvalid
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, ErrInvalid
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, ErrInvalid
		}
		fields[key] = value
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalid
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing data", ErrInvalid)
	}
	return fields, nil
}
