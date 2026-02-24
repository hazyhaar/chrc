package domregistry

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"time"
)

// GenerateLeaderboardHTML produces a static HTML page with domain and instance leaderboards.
func (r *Registry) GenerateLeaderboardHTML(ctx context.Context) ([]byte, error) {
	domains, err := r.DomainLeaderboard(ctx, 100)
	if err != nil {
		return nil, fmt.Errorf("domain leaderboard: %w", err)
	}

	instances, err := r.InstanceLeaderboard(ctx, 100)
	if err != nil {
		return nil, fmt.Errorf("instance leaderboard: %w", err)
	}

	stats, err := r.Stats(ctx)
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>DOM Registry — Community Leaderboard</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:system-ui,-apple-system,sans-serif;background:#f8f9fa;color:#212529;max-width:960px;margin:0 auto;padding:2rem 1rem}
h1{font-size:1.5rem;margin-bottom:.5rem}
h2{font-size:1.2rem;margin:2rem 0 .75rem;border-bottom:2px solid #dee2e6;padding-bottom:.25rem}
.stats{display:flex;gap:1rem;margin-bottom:2rem}
.stat{background:#fff;border:1px solid #dee2e6;border-radius:.5rem;padding:1rem;flex:1;text-align:center}
.stat-num{font-size:1.5rem;font-weight:700;color:#495057}
.stat-label{font-size:.8rem;color:#868e96;text-transform:uppercase}
table{width:100%;border-collapse:collapse;background:#fff;border:1px solid #dee2e6;border-radius:.5rem;overflow:hidden;margin-bottom:2rem}
th{background:#e9ecef;padding:.5rem .75rem;text-align:left;font-size:.85rem;font-weight:600}
td{padding:.5rem .75rem;border-top:1px solid #dee2e6;font-size:.85rem}
tr:hover td{background:#f1f3f5}
.success-high{color:#2b8a3e}
.success-mid{color:#e67700}
.success-low{color:#c92a2a}
.badge{display:inline-block;padding:.1rem .4rem;border-radius:.25rem;font-size:.75rem;font-weight:600}
.badge-good{background:#d3f9d8;color:#2b8a3e}
.badge-warn{background:#fff3bf;color:#e67700}
.badge-bad{background:#ffe3e3;color:#c92a2a}
.generated{text-align:center;font-size:.75rem;color:#868e96;margin-top:2rem}
</style>
</head>
<body>
<h1>DOM Registry — Community Leaderboard</h1>
`)

	// Stats
	fmt.Fprintf(&buf, `<div class="stats">
<div class="stat"><div class="stat-num">%d</div><div class="stat-label">Profiles</div></div>
<div class="stat"><div class="stat-num">%d</div><div class="stat-label">Corrections</div></div>
<div class="stat"><div class="stat-num">%d</div><div class="stat-label">Reports</div></div>
</div>
`, stats.Profiles, stats.Corrections, stats.Reports)

	// Domain leaderboard
	buf.WriteString(`<h2>Domain Reliability</h2>
<table>
<thead><tr><th>#</th><th>Domain</th><th>Success Rate</th><th>Profiles</th><th>Uses</th><th>Repairs</th><th>Updated</th></tr></thead>
<tbody>
`)

	for i, d := range domains {
		cls := "success-high"
		badge := "badge-good"
		if d.AvgSuccess < 0.5 {
			cls = "success-low"
			badge = "badge-bad"
		} else if d.AvgSuccess < 0.8 {
			cls = "success-mid"
			badge = "badge-warn"
		}
		ts := time.UnixMilli(d.LastUpdated).UTC().Format("2006-01-02")
		fmt.Fprintf(&buf, `<tr><td>%d</td><td>%s</td><td class="%s"><span class="badge %s">%.0f%%</span></td><td>%d</td><td>%d</td><td>%d</td><td>%s</td></tr>
`,
			i+1, html.EscapeString(d.Domain), cls, badge, d.AvgSuccess*100,
			d.ProfileCount, d.TotalUses, d.TotalRepairs, ts)
	}

	buf.WriteString(`</tbody></table>
`)

	// Instance leaderboard
	buf.WriteString(`<h2>Instance Contributors</h2>
<table>
<thead><tr><th>#</th><th>Instance</th><th>Accepted</th><th>Rejected</th><th>Pending</th><th>Domains</th><th>Ratio</th></tr></thead>
<tbody>
`)

	for i, inst := range instances {
		total := inst.CorrectionsAccepted + inst.CorrectionsRejected
		var ratio string
		var badge string
		if total > 0 {
			rate := float64(inst.CorrectionsAccepted) / float64(total)
			ratio = fmt.Sprintf("%.0f%%", rate*100)
			if rate >= 0.8 {
				badge = "badge-good"
			} else if rate >= 0.5 {
				badge = "badge-warn"
			} else {
				badge = "badge-bad"
			}
		} else {
			ratio = "—"
			badge = "badge-warn"
		}
		fmt.Fprintf(&buf, `<tr><td>%d</td><td><code>%s</code></td><td>%d</td><td>%d</td><td>%d</td><td>%d</td><td><span class="badge %s">%s</span></td></tr>
`,
			i+1, html.EscapeString(inst.InstanceID),
			inst.CorrectionsAccepted, inst.CorrectionsRejected, inst.CorrectionsPending,
			inst.DomainsCovered, badge, ratio)
	}

	buf.WriteString(`</tbody></table>
`)

	fmt.Fprintf(&buf, `<div class="generated">Generated %s</div>
</body>
</html>
`, time.Now().UTC().Format("2006-01-02 15:04:05 UTC"))

	return buf.Bytes(), nil
}
