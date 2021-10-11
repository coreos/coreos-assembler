---
nav_exclude: true
---

# Guide to update the build process SVG

<!-- The path to this SVG is fixed here to make sure that it is correctly
displayed both on the coreos.github.io website rendered by Jekyll and on
github.com while browsing the repository view. The main downside here is that
this will not be updated for branches -->
<img src="https://coreos.github.io/coreos-assembler/build-process.svg?sanitize=true">

- Go to [mermaid-live-editor](https://mermaidjs.github.io/mermaid-live-editor)
- Copy paste script:

```mermaid
graph LR
 A(<div><h3>FCOS Config</h3><ul><li>rpm-ostree treefile</li><li>Repos</li><li>Misc other bits</li></ul>)  --> C[<h3>coreos-assembler container</h3><ul><li>Fedora Base</li><li>Build Script Installed</li></ul></div>];
D[RPMS] --> C;
E[<h3>coreos-assembler repo </h3> <ul><li>Build Script</li><li>Dockerfile</li></ul>]  --Build on quay.io --> C;
C --> F[Volume mounted in <ul><li>Disk Images</li>ostree commit</li>.</ul>]

classDef fcos fill:#9f6,stroke:#333,stroke-width:1px, width:300px, padding:30px;
class A fcos;
classDef coreos-assembler-con fill:#53a3da,stroke-width:1px, color:#fff;
class C coreos-assembler-con;
classDef coreos-assembler-repo fill:#53a3da,stroke-width:1px, color:#fff;
class E coreos-assembler-con;
classDef rpms fill:#ef4d5b,stroke-width:1px, color:#fff;
class D rpms;
```

- Export to Svg and update `build-process.svg`
