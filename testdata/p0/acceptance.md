---
title: P0 integration acceptance
printout:
  size: B5
  orientation: portrait
  margin: 18mm 15mm
  insideMargin: 20mm
  outsideMargin: 14mm
  header: P0 acceptance
  pageNumbers: true
  chapterStart: right
  expected_pages: 48
---

# P0 integration acceptance

See @ref(diagram), @ref(trend), and @ref(summary).

$$$
\begin{aligned}
profit &= revenue - cost \\
margin &= \begin{cases}
profit / revenue & revenue > 0 \\
0 & revenue = 0
\end{cases}
\end{aligned}
$$$

@pagebreak

@figure(id="diagram" src="figure.svg" alt="Integration diagram" caption="Pipeline overview" source="P0 fixture")

@chart(type="line" path="data.csv" x="month" y="profit" series="venue" id="trend" caption="Deterministic trend" xLabel="Month" yLabel="Profit" yUnit="JPY" note="Generated from CSV")

@table(id="summary" caption="Summary values" source="P0 fixture")
| Venue | Profit |
| --- | ---: |
| Hall A | 34 |
| Hall B | 26 |
