// Copyright 2024 The Carvel Authors.
// SPDX-License-Identifier: Apache-2.0

package packageinstall

import (
	"fmt"
	"sort"
	"strings"

	"carvel.dev/kapp-controller/pkg/apis/kappctrl/v1alpha1"
	kcv1alpha1 "carvel.dev/kapp-controller/pkg/apis/kappctrl/v1alpha1"
	pkgingv1alpha1 "carvel.dev/kapp-controller/pkg/apis/packaging/v1alpha1"
	datapkgingv1alpha1 "carvel.dev/kapp-controller/pkg/apiserver/apis/datapackaging/v1alpha1"
	"carvel.dev/kapp-controller/pkg/client/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	ManuallyControlledAnnKey = "ext.packaging.carvel.dev/manually-controlled"

	HelmTemplateOverlayNameKey      = "ext.packaging.carvel.dev/helm-template-name"
	HelmTemplateOverlayNameSpaceKey = "ext.packaging.carvel.dev/helm-template-namespace"

	// Resulting secret names are sorted deterministically by suffix
	ExtYttPathsFromSecretNameAnnKey  = "ext.packaging.carvel.dev/ytt-paths-from-secret-name"
	ExtHelmPathsFromSecretNameAnnKey = "ext.packaging.carvel.dev/helm-template-values-from-secret-name"

	ExtYttDataValuesOverlaysAnnKey = "ext.packaging.carvel.dev/ytt-data-values-overlays"

	ExtFetchSecretNameAnnKeyFmt = "ext.packaging.carvel.dev/fetch-%d-secret-name"
)

// NewApp creates a new instance of v1alpha1.App based on the provided parameters.
// It takes an existingApp, pkgInstall, pkgVersion, and opts, and returns the newly created App.
func NewApp(existingApp *v1alpha1.App, pkgInstall *pkgingv1alpha1.PackageInstall, pkgVersion datapkgingv1alpha1.Package, opts Opts) (*v1alpha1.App, error) {
	desiredApp := existingApp.DeepCopy()

	if _, found := existingApp.Annotations[ManuallyControlledAnnKey]; found {
		// Skip all updates to App CR if annotation is present
		return desiredApp, nil
	}

	desiredApp.Name = pkgInstall.Name
	desiredApp.Namespace = pkgInstall.Namespace

	if desiredApp.Annotations == nil {
		desiredApp.Annotations = map[string]string{}
	}
	desiredApp.Annotations["packaging.carvel.dev/package-ref-name"] = pkgVersion.Spec.RefName
	desiredApp.Annotations["packaging.carvel.dev/package-version"] = pkgVersion.Spec.Version

	desiredApp.Spec = *pkgVersion.Spec.Template.Spec
	desiredApp.Spec.ServiceAccountName = pkgInstall.Spec.ServiceAccountName
	if pkgInstall.Spec.SyncPeriod == nil {
		desiredApp.Spec.SyncPeriod = &metav1.Duration{Duration: opts.DefaultSyncPeriod}
	} else {
		desiredApp.Spec.SyncPeriod = pkgInstall.Spec.SyncPeriod
	}
	desiredApp.Spec.NoopDelete = pkgInstall.Spec.NoopDelete
	desiredApp.Spec.Paused = pkgInstall.Spec.Paused
	desiredApp.Spec.Canceled = pkgInstall.Spec.Canceled
	desiredApp.Spec.Cluster = pkgInstall.Spec.Cluster
	desiredApp.Spec.DefaultNamespace = pkgInstall.Spec.DefaultNamespace

	err := controllerutil.SetControllerReference(pkgInstall, desiredApp, scheme.Scheme)
	if err != nil {
		return &v1alpha1.App{}, err
	}

	for i, fetchStep := range desiredApp.Spec.Fetch {
		annKey := fmt.Sprintf(ExtFetchSecretNameAnnKeyFmt, i)

		secretName, found := pkgInstall.Annotations[annKey]
		if !found {
			continue
		}

		secretRef := &kcv1alpha1.AppFetchLocalRef{Name: secretName}
		switch {
		case fetchStep.Inline != nil:
			// do nothing
		case fetchStep.Image != nil:
			desiredApp.Spec.Fetch[i].Image.SecretRef = secretRef
		case fetchStep.HTTP != nil:
			desiredApp.Spec.Fetch[i].HTTP.SecretRef = secretRef
		case fetchStep.Git != nil:
			desiredApp.Spec.Fetch[i].Git.SecretRef = secretRef
		case fetchStep.HelmChart != nil:
			if desiredApp.Spec.Fetch[i].HelmChart.Repository != nil {
				desiredApp.Spec.Fetch[i].HelmChart.Repository.SecretRef = secretRef
			}
		case fetchStep.ImgpkgBundle != nil:
			desiredApp.Spec.Fetch[i].ImgpkgBundle.SecretRef = secretRef
		default:
			// do nothing
		}
	}

	valuesApplied := false
	yttPathsApplied := false
	helmPathsApplied := false

	for i, templateStep := range desiredApp.Spec.Template {
		if templateStep.HelmTemplate != nil {
			if !helmPathsApplied {
				helmPathsApplied = true

				if _, found := pkgInstall.Annotations[HelmTemplateOverlayNameKey]; found {
					templateStep.HelmTemplate.Name = pkgInstall.Annotations[HelmTemplateOverlayNameKey]
				}
				if _, found := pkgInstall.Annotations[HelmTemplateOverlayNameSpaceKey]; found {
					templateStep.HelmTemplate.Namespace = pkgInstall.Annotations[HelmTemplateOverlayNameSpaceKey]
				}
				for _, secretName := range secretNamesFromAnn(pkgInstall, ExtHelmPathsFromSecretNameAnnKey) {
					templateStep.HelmTemplate.ValuesFrom = append(templateStep.HelmTemplate.ValuesFrom, kcv1alpha1.AppTemplateValuesSource{
						SecretRef: &kcv1alpha1.AppTemplateValuesSourceRef{
							Name: secretName,
						},
					})
				}
			}
			if !valuesApplied {
				valuesApplied = true

				for _, value := range pkgInstall.Spec.Values {
					templateStep.HelmTemplate.ValuesFrom = append(templateStep.HelmTemplate.ValuesFrom, kcv1alpha1.AppTemplateValuesSource{
						SecretRef: &kcv1alpha1.AppTemplateValuesSourceRef{
							Name: value.SecretRef.Name,
						},
					})
				}
			}
		}

		if templateStep.Ytt != nil {
			if !yttPathsApplied {
				yttPathsApplied = true

				for _, secretName := range secretNamesFromAnn(pkgInstall, ExtYttPathsFromSecretNameAnnKey) {
					if templateStep.Ytt.Inline == nil {
						templateStep.Ytt.Inline = &kcv1alpha1.AppFetchInline{}
					}
					templateStep.Ytt.Inline.PathsFrom = append(templateStep.Ytt.Inline.PathsFrom, kcv1alpha1.AppFetchInlineSource{
						SecretRef: &kcv1alpha1.AppFetchInlineSourceRef{
							Name: secretName,
						},
					})
				}
			}

			if !valuesApplied {
				valuesApplied = true

				if _, found := pkgInstall.Annotations[ExtYttDataValuesOverlaysAnnKey]; found {
					if templateStep.Ytt.Inline == nil {
						templateStep.Ytt.Inline = &kcv1alpha1.AppFetchInline{}
					}
					for _, value := range pkgInstall.Spec.Values {
						templateStep.Ytt.Inline.PathsFrom = append(templateStep.Ytt.Inline.PathsFrom, kcv1alpha1.AppFetchInlineSource{
							SecretRef: &kcv1alpha1.AppFetchInlineSourceRef{
								Name: value.SecretRef.Name,
							},
						})
					}
				} else {
					for _, value := range pkgInstall.Spec.Values {
						templateStep.Ytt.ValuesFrom = append(templateStep.Ytt.ValuesFrom, kcv1alpha1.AppTemplateValuesSource{
							SecretRef: &kcv1alpha1.AppTemplateValuesSourceRef{
								Name: value.SecretRef.Name,
							},
						})
					}
				}
			}
		}

		desiredApp.Spec.Template[i] = templateStep
	}

	return desiredApp, nil
}

func secretNamesFromAnn(installedPkg *pkgingv1alpha1.PackageInstall, annKey string) []string {
	var suffixes []string
	suffixToSecretName := map[string]string{}

	for ann, secretName := range installedPkg.Annotations {
		if ann == annKey {
			suffix := ""
			suffixToSecretName[suffix] = secretName
			suffixes = append(suffixes, suffix)
		} else if strings.HasPrefix(ann, annKey+".") {
			suffix := strings.TrimPrefix(ann, annKey+".")
			suffixToSecretName[suffix] = secretName
			suffixes = append(suffixes, suffix)
		}
	}

	sort.Strings(suffixes)

	var result []string
	for _, suffix := range suffixes {
		result = append(result, suffixToSecretName[suffix])
	}
	return result
}
