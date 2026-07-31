---
title: Render metadata fixture
owners: [alice, bob]
project: karte-format
---
# Metadata contract

@import(type="md" path="partials/summary.md")

@import(type="csv" path="data/metrics.csv" select="Metric,Value")

@import(type="tex" path="math/model.tex" display="block")

[Specification](https://example.com/spec)
[Local notes](notes.md#details)
[Summary](#summary)
[Team](mailto:team@example.com)
<https://example.com/autolink>
https://example.com/bare
<team@example.com>
www.example.com
[Refresh](?v=1)
[Self]()

![Diagram](assets/diagram.svg)
![Missing](assets/missing.png)
![Remote](https://example.com/logo.png)
![Embedded](data:image/gif;base64,R0lGODlhAQABAAAAACw=)
