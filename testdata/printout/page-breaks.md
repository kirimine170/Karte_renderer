---
title: Page break fixture
printout:
  size: B5
  margin: 18mm 15mm
  pageNumbers: true
---

# First page

This paragraph is on the first page.

@pagebreak

# Second page

| Item | Value |
| --- | ---: |
| A | 1 |

@pagebreak(after)

# Third page

The final page verifies explicit after-break behavior.
