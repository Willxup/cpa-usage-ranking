package reports

import (
	"crypto/sha256"
	"encoding/json"
	"math"
	"sort"

	"cpa-usage-ranking/internal/models"
)

// Prepared is a validated, immutable-by-caller snapshot of the complete
// input. Digesting it does not depend on any catalog, price or database.
type Prepared struct {
	revision int64
	items    []Item
	digest   [sha256.Size]byte
}

func (p Prepared) Revision() int64           { return p.revision }
func (p Prepared) Digest() [sha256.Size]byte { return p.digest }

// Prepare performs only catalog-independent checks. In particular, it must
// succeed before looking up the current revision, without aggregating models.
func Prepare(report Report) (Prepared, error) {
	if err := Validate(report); err != nil {
		return Prepared{}, err
	}
	items := make([]Item, len(report.Items))
	copy(items, report.Items)
	seen := make(map[struct {
		model string
		tier  models.Tier
	}]bool, len(items))
	for i := range items {
		item := &items[i]
		if _, err := models.NormalizeReportedName(item.Model); err != nil || item.Tier > models.Flex {
			return Prepared{}, ErrInvalid
		}
		key := struct {
			model string
			tier  models.Tier
		}{item.Model, item.Tier}
		if seen[key] {
			return Prepared{}, ErrInvalid
		}
		seen[key] = true
		// Copy pointers and normalize signed zero, so semantically equal JSON
		// numeric spellings and later caller mutations cannot change the input.
		if item.TTFTAvgMS != nil {
			v := *item.TTFTAvgMS
			if v == 0 {
				v = 0
			}
			item.TTFTAvgMS = &v
		}
		if item.LatencyAvgMS != nil {
			v := *item.LatencyAvgMS
			if v == 0 {
				v = 0
			}
			item.LatencyAvgMS = &v
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Model != items[j].Model {
			return items[i].Model < items[j].Model
		}
		return items[i].Tier < items[j].Tier
	})
	encoded, err := json.Marshal(struct {
		SchemaVersion int
		Revision      int64
		Items         []Item
	}{1, report.Revision, items})
	if err != nil {
		return Prepared{}, ErrInvalid
	}
	return Prepared{revision: report.Revision, items: items, digest: sha256.Sum256(encoded)}, nil
}

// Key identifies a stored row. ModelID 0 and tier default are reserved for
// the all-model total; real catalog model IDs are positive.
type Key struct {
	ModelID int64
	Tier    models.Tier
}

// Aggregate uses the current registry only for a new replacement. Valid
// unknown and zero-execution items remain in the digest but not the rows.
func (p Prepared) Aggregate(registry *models.Registry) (map[Key]Metrics, error) {
	rows := make(map[Key]Metrics)
	var total Metrics
	for _, item := range p.items {
		resolved, err := registry.Resolve(item.Model)
		if err != nil {
			return nil, ErrInvalid
		}
		if !resolved.Matched || item.RequestCount == 0 {
			continue
		}
		metric, err := itemMetrics(item)
		if err != nil {
			return nil, ErrInvalid
		}
		key := Key{ModelID: resolved.ID, Tier: item.Tier}
		merged, ok := addMetrics(rows[key], metric)
		if !ok {
			return nil, ErrInvalid
		}
		rows[key] = merged
		total, ok = addMetrics(total, metric)
		if !ok {
			return nil, ErrInvalid
		}
	}
	if total.RequestCount != 0 {
		rows[Key{}] = total
	}
	return rows, nil
}

func addMetrics(a, b Metrics) (Metrics, bool) {
	pairs := []struct {
		dst   *int64
		value int64
	}{
		{&a.TotalTokens, b.TotalTokens}, {&a.InputTokens, b.InputTokens}, {&a.OutputTokens, b.OutputTokens},
		{&a.CacheReadTokens, b.CacheReadTokens}, {&a.CacheWriteTokens, b.CacheWriteTokens}, {&a.ReasoningTokens, b.ReasoningTokens},
		{&a.RequestCount, b.RequestCount}, {&a.SuccessCount, b.SuccessCount},
		{&a.TTFTSamples, b.TTFTSamples}, {&a.LatencySamples, b.LatencySamples},
	}
	for _, pair := range pairs {
		var ok bool
		*pair.dst, ok = addInt(*pair.dst, pair.value)
		if !ok {
			return Metrics{}, false
		}
	}
	a.TTFTSumMS += b.TTFTSumMS
	a.LatencySumMS += b.LatencySumMS
	if math.IsInf(a.TTFTSumMS, 0) || math.IsNaN(a.TTFTSumMS) || math.IsInf(a.LatencySumMS, 0) || math.IsNaN(a.LatencySumMS) {
		return Metrics{}, false
	}
	return a, true
}
