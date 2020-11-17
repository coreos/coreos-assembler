package ocp

var (
	// podmanCaps are the specific permissions we needed to run a podman
	// pod. This is a privileged pod.
	podmanCaps = []string{
		"CAP_DAC_READ_SEARCH",
		"CAP_LINUX_IMMUTABLE",
		"CAP_NET_BROADCAST",
		"CAP_NET_ADMIN",
		"CAP_IPC_LOCK",
		"CAP_IPC_OWNER",
		"CAP_SYS_MODULE",
		"CAP_SYS_RAWIO",
		"CAP_SYS_PTRACE",
		"CAP_SYS_PACCT",
		"CAP_SYS_ADMIN",
		"CAP_SYS_BOOT",
		"CAP_SYS_NICE",
		"CAP_SYS_RESOURCE",
		"CAP_SYS_TIME",
		"CAP_SYS_TTY_CONFIG",
		"CAP_LEASE",
		"CAP_AUDIT_CONTROL",
		"CAP_MAC_OVERRIDE",
		"CAP_MAC_ADMIN",
		"CAP_SYSLOG",
		"CAP_WAKE_ALARM",
		"CAP_BLOCK_SUSPEND",
		"CAP_AUDIT_READ",
	}
)
