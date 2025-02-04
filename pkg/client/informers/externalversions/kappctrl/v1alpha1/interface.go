// Code generated by main. DO NOT EDIT.

package v1alpha1

import (
	internalinterfaces "carvel.dev/kapp-controller/pkg/client/informers/externalversions/internalinterfaces"
)

// Interface provides access to all the informers in this group version.
type Interface interface {
	// Apps returns a AppInformer.
	Apps() AppInformer
}

type version struct {
	factory          internalinterfaces.SharedInformerFactory
	namespace        string
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// New returns a new Interface.
func New(f internalinterfaces.SharedInformerFactory, namespace string, tweakListOptions internalinterfaces.TweakListOptionsFunc) Interface {
	return &version{factory: f, namespace: namespace, tweakListOptions: tweakListOptions}
}

// Apps returns a AppInformer.
func (v *version) Apps() AppInformer {
	return &appInformer{factory: v.factory, namespace: v.namespace, tweakListOptions: v.tweakListOptions}
}
