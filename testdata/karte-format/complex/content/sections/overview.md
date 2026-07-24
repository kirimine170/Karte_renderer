## Overview

This section combines nested Markdown, selected CSV columns, and inline TeX.

@import(type="csv" path="../../data/summary.csv" select="resource,count")

Energy formula:
@import(type="tex" path="../../math/energy.tex" display="inline")
