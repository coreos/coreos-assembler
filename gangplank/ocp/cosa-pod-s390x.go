// +build s390x

package ocp

import resource "k8s.io/apimachinery/pkg/api/resource"

/*
	s390x/Z is a bit greedy...
*/

func init() {
	baseMem = *resource.NewQuantity(12*1024*1024*1024, resource.BinarySI)
}
