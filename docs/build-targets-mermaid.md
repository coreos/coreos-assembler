---
nav_exclude: true
---

# Guide to update the build targets SVG

<!-- The path to this SVG is fixed here to make sure that it is correctly
displayed both on the coreos.github.io website rendered by Jekyll and on
github.com while browsing the repository view. The main downside here is that
this will not be updated for branches -->
<img src="https://coreos.github.io/coreos-assembler/build-targets.svg?sanitize=true">

- Go to [mermaid-live-editor](https://mermaidjs.github.io/mermaid-live-editor)
- Copy paste script:

```mermaid
graph LR
  S([Start]) --> O[ostree]
  subgraph "Builds ostree automatically"
    O --> Q[qemu]
    O --> M[metal]
  end
  M --> M4k[metal4k]
  M4k --> L[live]
```

- Export to Svg and update `build-targets.svg`
