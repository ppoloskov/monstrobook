# Audiobookshelf resource comparison, 2026-11-21

Data cutoff: 2026-11-21 15:58 Australia/Brisbane. Cluster: `au`, workload:
`media/abooks`. This is observational production telemetry, not a controlled
benchmark. The requested 123-day lookback begins 2026-07-21. VictoriaMetrics
is configured with `--retentionPeriod=30d`; no four-month comparison is
available from this store. The earliest matching pod discovered in this query
was 2026-10-27 17:20. An earlier snapshot retains older summarized results in
[the 2026-11-10 report](../archive/reports/ABOOKS-RESOURCE-REPORT-2026-11-10.md).

## Comparable longer-lived pods

CPU is mCPU, computed from a five-minute rate; memory is MiB. Percentiles and
maxima are calculated over sampled values, not individual requests. First and
last observed times are approximate. Hours span the first to last observation,
and can include gaps. Rows are individual pods, not image-wide aggregates.

| Image | First observed | Hours | CPU avg / p50 / p95 | RSS p50 / p95 | Working set p50 / p95 | Cache p50 / p95 | Restarts |
|---|---|---:|---:|---:|---:|---:|---:|
| TS 2.37.2, stable pod `hpsjf` | Oct 29 22:10 | 107.5 | 1.60 / 0.29 / 7.57 | 68.4 / 126.1 | 88.4 / 218.2 | 142.7 / 241.6 | 0 |
| Go `sha-4616244`, `tqq2k` | Nov 3 19:10 | 160.8 | 7.30 / 0.15 / 80.72 | 29.5 / 37.3 | 68.4 / 112.3 | 2217.5 / 2369.3 | 0 |
| Go `sha-b42969b`, `fndnk` | Nov 10 12:10 | 71.5 | 0.52 / 0.08 / 3.34 | 23.1 / 31.5 | 93.9 / 108.8 | 60.9 / 204.5 | 6 |
| Go `sha-b42969b`, `9hgw8` | Nov 13 16:40 | 116.7 | 0.63 / 0.11 / 4.48 | 22.9 / 28.4 | 43.2 / 60.8 | 94.6 / 284.7 | 0 |
| Monstrobook `sha-54997df`, `x2t7g` | Nov 20 23:30 | 14.5 | 0.41 / 0.09 / 3.91 | 18.0 / 29.1 | 20.6 / 30.6 | 56.4 / 86.4 | 0 |

The latest observed running Monstrobook pod averaged about 74% less CPU and
had about 74% lower median RSS than the stable TS pod. Its observed window is
only 14.5 hours versus 107.5 hours for TS; workloads, restarts, cache state and
time of day were not controlled. It is too early to attribute all differences
to implementation or treat this as a benchmark. The earlier Go build had a
much higher CPU p95 and cache; the previous report links this to its hourly
scanner using application logs. The later Go build had six restarts on `fndnk`;
the historical termination-reason series included `Completed` and `Unknown`,
which do not establish the cause of all six.

TS 2.36.0 churn pod `lx2c9` had 38.3 hours observed, 2.68 mCPU average,
101.7 MiB median RSS, 136.2 MiB median working set and 13 restarts at a 1 GiB
limit. Its stored last-termination reason included `OOMKilled`. The short
TS 2.37.2 first rollout had 11 restarts at a 2 GiB limit; see the prior report
for its caveats on OOM attribution. Different limits and churn make these
poor steady-state baselines.

## Current availability

At approximately 15:58 on Nov 21, `media/abooks` was 0/1 available. Pod
`abooks-58b7fdc79b-q9sfl` was in `ImagePullBackOff`: kubelet reported the
configured `ghcr.io/ppoloskov/monstrobook:sha-7246e26` image was not found.
This image did not run in the observed metrics and cannot be assessed here.
No deployment changes were made.

## Reproduction and limitations

The existing [VictoriaMetrics procedure](../archive/reports/ABOOKS-RESOURCE-REPORT-2026-11-10.md#how-to-get-data-for-future-comparison)
documents the port-forward, queries, and interpretation. The current helper is
`tools/abooks-resource-report.py`, not the historical `go-backend/tools` path.
Its fixed 300-second pod discovery step exceeds VictoriaMetrics' 30,000-point
limit at 123 days. For this report a temporary copy used a 600-second discovery
step; per-pod resource queries retained the helper's default 60-second step.
The 600-second discovery interval can miss short-lived pods and up to roughly
ten minutes at either end. Series can have missing scrapes; the helper excludes
zero values from statistics and treats missing restart/limit series as zero or
unknown, so verify any critical result independently. Restart counts are the
maximum observed pod counter, not necessarily restarts within the window.
Cache and working set measure different cgroup memory accounting categories;
they must not be added as independent memory totals. CPU rate smooths bursts
over five minutes and is not scan duration.

## Operation timing and traffic

The previous report measured the stable TS 2.37.2 pod's 107 hourly library
scans from application logs: 1.06 s mean, 0.88 s median, 1.15 s p95 and
13.86 s maximum. It recorded approximately 24 s average for the older Go
`sha-4616244` scan and Go `sha-b42969b` daily logs rounded to 0–1 s. Those
are not measurements of the newer Monstrobook `sha-54997df` build. No measured
TS-versus-Monstrobook API, playback, boot or scan latency was found; the
[performance plan](PERF_TEST_PLAN.md) is a plan, not benchmark results.

The following mean network rates were queried at 60-second steps using a
five-minute `rate` of cAdvisor pod network counters over the same approximate
pod windows above. These are decimal kB/s, across the pod interface, not a
count of requests or a direct measure of equivalent work:

| Pod | Receive kB/s | Transmit kB/s |
|---|---:|---:|
| TS 2.37.2 `hpsjf` | 4.37 | 3.20 |
| Go `sha-4616244` `tqq2k` | 3.34 | 2.68 |
| Go `sha-b42969b` `9hgw8` | 3.47 | 0.71 |
| Monstrobook `sha-54997df` `x2t7g` | 2.70 | 0.69 |

Different traffic volumes reinforce that the resource comparison is not a
controlled performance test. No Audiobookshelf request-duration or request
count metric was identified in the available VictoriaMetrics metric names;
the HTTP response-time series found was for a separate `ip.me` probe.

## Update: `sha-4a193e2`, 2026-11-22

At 17:43 Brisbane, pod `abooks-cb4958489-2kbc8` was 1/1 Running with zero
restarts. Direct cAdvisor queries at 60-second steps covered Nov 21 16:10 to
Nov 22 17:43, with 1,534 samples and no gaps longer than one minute. CPU uses
the five-minute rate; values below include zeros and use the same percentile
method as the helper.

| Metric | Mean | p50 | p95 | Maximum |
|---|---:|---:|---:|---:|
| CPU (mCPU) | 0.45 | 0.11 | 3.80 | 8.81 |
| RSS (MiB) | 19.40 | 17.83 | 22.67 | 70.95 |
| Working set (MiB) | 22.64 | 21.34 | 27.04 | 74.45 |
| Cache (MiB) | 88.55 | 85.62 | 151.14 | 152.01 |

`kubectl top` showed 1 mCPU and 21 MiB at the time of the query. The helper's
pod-discovery series had only five samples across 16.6 hours, so its inferred
lifetime omitted substantial recorded container data; do not use that helper
output for this pod without independently checking discovery coverage. The
comparison remains observational and workloads are not controlled.
