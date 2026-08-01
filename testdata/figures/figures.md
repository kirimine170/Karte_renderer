---
title: Figure reference fixture
printout:
  size: B5
  margin: 16mm
  pageNumbers: true
---

# Figure references

The overview is shown in @ref(overview), results in @ref(results), and the trend in @ref(trend).

@figure(id="overview" src="figure.svg" alt="Overview" caption="System overview" source="Internal model" note="Simplified diagram")

@pagebreak

# Table reference

@table(id="results" caption="Estimated results" source="Performance log" note="Values are illustrative")
| Item | Value |
| --- | ---: |
| A | 10 |
| B | 20 |

@pagebreak

# Chart reference

@chart(type="line" path="data.csv" x="x" y="y" id="trend" title="Measured trend" caption="Trend by observation" source="Performance log")
