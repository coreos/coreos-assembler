package platform

import "fmt"

type GenericInstanceType struct {
	// Name is a string used to identify this type
	Name string
	// Cpu is a count of exposed host vCPUs
	Cpu uint
	// Memory is in mebibytes
	Memory uint
}

var InstanceNano = GenericInstanceType{Name: "nano", Cpu: 1, Memory: 512}
var InstanceMicro = GenericInstanceType{Name: "micro", Cpu: 1, Memory: 1024}
var InstanceSmall = GenericInstanceType{Name: "small", Cpu: 1, Memory: 2 * 1024}
var InstanceMedium = GenericInstanceType{Name: "medium", Cpu: 2, Memory: 4 * 1024}
var InstanceLarge = GenericInstanceType{Name: "large", Cpu: 2, Memory: 8 * 1024}
var InstanceXlarge = GenericInstanceType{Name: "xlarge", Cpu: 4, Memory: 16 * 1024}

var InstanceTypes = []GenericInstanceType{InstanceNano, InstanceMicro, InstanceSmall, InstanceMedium, InstanceLarge, InstanceXlarge}

func LookupInstanceType(t string) (*GenericInstanceType, error) {
	for _, it := range InstanceTypes {
		if t == it.Name {
			return &it, nil
		}
	}
	return nil, fmt.Errorf("unknown instance type: %s", t)
}
