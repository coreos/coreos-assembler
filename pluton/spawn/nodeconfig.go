package spawn

var nodeTmpl = `#cloud-config
coreos:
  units: 
    - name: "update-engine.service"
      mask: true
    - name: "locksmithd.service"
      mask: true
    - name: "kubelet.service"
      enable: false
      content: | 
{{.KubeletService}}`
