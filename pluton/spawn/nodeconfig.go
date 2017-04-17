package spawn

var nodeTmpl = `#cloud-config
coreos:
  units: 
    - name: kubelet.service
      enable: false
      content: | 
{{.KubeletService}}
  update:
    reboot-strategy: "off"`
