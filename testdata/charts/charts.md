---
title: Deterministic chart fixture
printout:
  size: B5
  margin: 14mm
  pageNumbers: true
---

# Scatter

@chart(type="scatter" path="performance.csv" x="attendance" y="profit" series="venue" title="Attendance and profit" xLabel="Attendance" xUnit="people" yLabel="Profit" yUnit="JPY")

@pagebreak

# Line

@chart(type="line" path="performance.csv" x="attendance" y="profit" series="venue" title="Profit curves" xLabel="Attendance" yLabel="Profit")

@pagebreak

# Bar

@chart(type="bar" path="performance.csv" x="month" y="profit" series="venue" title="Monthly profit" xLabel="Month" yLabel="Profit")

@pagebreak

# Histogram

@chart(type="histogram" path="performance.csv" x="profit" bins="5" title="Profit distribution" xLabel="Profit" yLabel="Count" note="Deterministic SVG fixture")
