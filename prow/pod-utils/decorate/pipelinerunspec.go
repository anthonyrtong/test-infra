/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package decorate

import (
	"fmt"
	"path/filepath"

	pipelinev1alpha1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"

	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	prowapi "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"k8s.io/test-infra/prow/clonerefs"
	"k8s.io/test-infra/prow/pod-utils/downwardapi"
	"k8s.io/test-infra/prow/pod-utils/wrapper"
)

const (
	startTaskName  = "start-task"
	finishTaskName = "finish-task"
)

func ProwJobToPipelineRun(pj prowapi.ProwJob, buildID string) (*pipelinev1alpha1.PipelineRun, error) {

	rawEnv, err := downwardapi.EnvForSpec(downwardapi.NewJobSpec(pj.Spec, buildID, pj.Name))
	if err != nil {
		return nil, err
	}

	spec := pj.Spec.PipelineRunSpec.DeepCopy()

	err = decoratePipelineSpec(spec, &pj, rawEnv, "")
	if err != nil {
		return nil, err
	}

	podLabels, annotations := LabelsAndAnnotationsForJob(pj)
	return &pipelinev1alpha1.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:        pj.ObjectMeta.Name,
			Labels:      podLabels,
			Annotations: annotations,
		},
		Spec: *spec,
	}, nil
}

func PipelineCloneRefs(pj prowapi.ProwJob, codeMountPath string, logMount coreapi.VolumeMount) (*coreapi.Container, []prowapi.Refs, []coreapi.Volume, error) {
	if pj.Spec.DecorationConfig == nil {
		return nil, nil, nil, nil
	}
	if skip := pj.Spec.DecorationConfig.SkipCloning; skip != nil && *skip {
		return nil, nil, nil, nil
	}
	var cloneVolumes []coreapi.Volume
	var refs []prowapi.Refs // Do not return []*prowapi.Refs which we do not own
	if pj.Spec.Refs != nil {
		refs = append(refs, *pj.Spec.Refs)
	}
	for _, r := range pj.Spec.ExtraRefs {
		refs = append(refs, r)
	}
	if len(refs) == 0 { // nothing to clone
		return nil, nil, nil, nil
	}

	var cloneMounts []coreapi.VolumeMount
	var sshKeyPaths []string
	for _, secret := range pj.Spec.DecorationConfig.SSHKeySecrets {
		volume, mount := sshVolume(secret)
		cloneMounts = append(cloneMounts, mount)
		sshKeyPaths = append(sshKeyPaths, mount.MountPath)
		cloneVolumes = append(cloneVolumes, volume)
	}

	var oauthMountPath string
	if pj.Spec.DecorationConfig.OauthTokenSecret != nil {
		oauthVolume, oauthMount := oauthVolume(pj.Spec.DecorationConfig.OauthTokenSecret.Name, pj.Spec.DecorationConfig.OauthTokenSecret.Key)
		cloneMounts = append(cloneMounts, oauthMount)
		oauthMountPath = filepath.Join(oauthMount.MountPath, oauthTokenFilename)
		cloneVolumes = append(cloneVolumes, oauthVolume)
	}

	volume, mount := tmpVolume("clonerefs-tmp")
	cloneMounts = append(cloneMounts, mount)
	cloneVolumes = append(cloneVolumes, volume)

	var cloneArgs []string
	var cookiefilePath string

	if cp := pj.Spec.DecorationConfig.CookiefileSecret; cp != "" {
		v, vm, vp := cookiefileVolume(cp)
		cloneMounts = append(cloneMounts, vm)
		cloneVolumes = append(cloneVolumes, v)
		cookiefilePath = vp
		cloneArgs = append(cloneArgs, "--cookiefile="+cookiefilePath)
	}

	env, err := cloneEnv(clonerefs.Options{
		CookiePath:       cookiefilePath,
		GitRefs:          refs,
		GitUserEmail:     clonerefs.DefaultGitUserEmail,
		GitUserName:      clonerefs.DefaultGitUserName,
		HostFingerprints: pj.Spec.DecorationConfig.SSHHostFingerprints,
		KeyFiles:         sshKeyPaths,
		Log:              CloneLogPath(logMount),
		SrcRoot:          codeMountPath,
		OauthTokenFile:   oauthMountPath,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("clone env: %v", err)
	}

	container := coreapi.Container{
		Name:         cloneRefsName,
		Image:        pj.Spec.DecorationConfig.UtilityImages.CloneRefs,
		Command:      []string{cloneRefsCommand},
		Args:         cloneArgs,
		Env:          env,
		VolumeMounts: append([]coreapi.VolumeMount{logMount}, cloneMounts...),
	}

	if pj.Spec.DecorationConfig.Resources != nil && pj.Spec.DecorationConfig.Resources.CloneRefs != nil {
		container.Resources = *pj.Spec.DecorationConfig.Resources.CloneRefs
	}
	return &container, refs, cloneVolumes, nil
}

func decoratePipelineSpec(spec *pipelinev1alpha1.PipelineRunSpec, pj *prowapi.ProwJob, rawEnv map[string]string, outputDir string) error {

	rawEnv[artifactsEnv] = artifactsPath
	rawEnv[gopathEnv] = codeMountPath
	logMount := coreapi.VolumeMount{
		Name:      logMountName,
		MountPath: logMountPath,
	}

	codeWorkspaceBinding := pipelinev1alpha1.WorkspaceBinding{
		Name: codeMountName,
		VolumeClaimTemplate: &coreapi.PersistentVolumeClaim{
			Spec: coreapi.PersistentVolumeClaimSpec{
				AccessModes: []coreapi.PersistentVolumeAccessMode{coreapi.ReadWriteOnce},
				Resources: coreapi.ResourceRequirements{
					Requests: coreapi.ResourceList{
						coreapi.ResourceStorage: resource.MustParse("5Gi"),
					},
				},
			},
		},
	}

	codeWorkspacePipelineTaskBinding := pipelinev1alpha1.WorkspacePipelineTaskBinding{
		Name:      codeMountName,
		Workspace: codeWorkspaceBinding.Name,
	}

	codeWorkspaceDeclaration := pipelinev1alpha1.WorkspaceDeclaration{
		Name:      codeMountName,
		MountPath: codeMountPath,
		ReadOnly:  false,
	}

	toolsMount := coreapi.VolumeMount{
		Name:      toolsMountName,
		MountPath: toolsMountPath,
	}

	var outputMount *coreapi.VolumeMount

	// TODO: Change to Workspaces for sharing storage secrets
	// TODO: Possibly change log volume and tools volume to be workspaces.  This could avoid extra place-entrypoint executions.
	_, blobStorageMounts, blobStorageOptions := BlobStorageOptions(*pj.Spec.DecorationConfig, false)

	encodedJobSpec := rawEnv[downwardapi.JobSpecEnv]

	startTask := pipelinev1alpha1.PipelineTask{
		Name:     startTaskName,
		TaskSpec: &pipelinev1alpha1.TaskSpec{},
	}
	startTask.TaskSpec.Steps = []pipelinev1alpha1.Step{}

	cloner, refs, cloneVolumes, err := PipelineCloneRefs(*pj, codeMountPath, logMount)
	if err != nil {
		return fmt.Errorf("create clonerefs container: %v", err)
	}
	var cloneLogMount *coreapi.VolumeMount
	if cloner != nil {
		cloneStep := pipelinev1alpha1.Step{
			Container: *cloner,
			Script:    "",
		}
		startTask.TaskSpec.Steps = append(startTask.TaskSpec.Steps, cloneStep)
		cloneLogMount = &logMount
	}

	initUpload, err := InitUpload(pj.Spec.DecorationConfig, blobStorageOptions, blobStorageMounts, cloneLogMount, outputMount, encodedJobSpec)
	if err != nil {
		return fmt.Errorf("create initupload container: %v", err)
	}

	initUploadStep := pipelinev1alpha1.Step{
		Container: *initUpload,
		Script:    "",
	}

	placeEntrypointStep := pipelinev1alpha1.Step{
		Container: PlaceEntrypoint(pj.Spec.DecorationConfig, toolsMount),
		Script:    "",
	}

	startTask.TaskSpec.Steps = append(
		startTask.TaskSpec.Steps,
		initUploadStep,
		placeEntrypointStep,
	)

	if len(refs) > 0 {
		spec.Workspaces = append(spec.Workspaces, codeWorkspaceBinding)
		for i, task := range spec.PipelineSpec.Tasks {
			for j := range task.TaskSpec.Steps {
				spec.PipelineSpec.Tasks[i].TaskSpec.Steps[j].WorkingDir = DetermineWorkDir(codeMountPath, refs)
			}
			spec.PipelineSpec.Tasks[i].Workspaces = append(task.Workspaces, codeWorkspacePipelineTaskBinding)
			spec.PipelineSpec.Tasks[i].TaskSpec.Workspaces = append(task.TaskSpec.Workspaces, codeWorkspaceDeclaration)
		}
		startTask.TaskSpec.Volumes = append(startTask.TaskSpec.Volumes, cloneVolumes...)
		startTask.Workspaces = append(startTask.Workspaces, codeWorkspacePipelineTaskBinding)
	}

	const (
		exitZero = true
	)

	for i, task := range spec.PipelineSpec.Tasks {

		var entries []wrapper.Options

		for j, step := range task.TaskSpec.Steps {
			if step.Name == "" {
				task.TaskSpec.Steps[j].Name = fmt.Sprintf("step-%d", j)
			}
			var previousMarker string
			if j > 0 {
				previousMarker = entries[j-1].MarkerFile
			}
			prefix := step.Name
			wrapperOptions, err := InjectEntrypoint(&spec.PipelineSpec.Tasks[i].TaskSpec.Steps[j].Container, pj.Spec.DecorationConfig.Timeout.Get(), pj.Spec.DecorationConfig.GracePeriod.Get(), prefix, previousMarker, exitZero, logMount, toolsMount)
			if err != nil {
				return fmt.Errorf("wrap container: %v", err)
			}
			entries = append(entries, *wrapperOptions)
		}
		sidecar, err := Sidecar(pj.Spec.DecorationConfig, blobStorageOptions, blobStorageMounts, logMount, outputMount, encodedJobSpec, !RequirePassingEntries, entries...)
		if err != nil {
			return fmt.Errorf("create sidecar: %v", err)
		}

		sidecarStep := pipelinev1alpha1.Step{
			Container: *sidecar,
			Script:    "",
		}
		spec.PipelineSpec.Tasks[i].TaskSpec.Steps = append(spec.PipelineSpec.Tasks[i].TaskSpec.Steps, sidecarStep)
	}

	finishTask := pipelinev1alpha1.PipelineTask{
		Name:     finishTaskName,
		TaskSpec: &pipelinev1alpha1.TaskSpec{},
	}

	sidecar, err := Sidecar(pj.Spec.DecorationConfig, blobStorageOptions, blobStorageMounts, logMount, outputMount, encodedJobSpec, !RequirePassingEntries)
	if err != nil {
		return fmt.Errorf("create sidecar: %v", err)
	}

	sidecarStep := pipelinev1alpha1.Step{
		Container: *sidecar,
		Script:    "",
	}

	finishTask.TaskSpec.Steps = []pipelinev1alpha1.Step{sidecarStep}

	for i, task := range spec.PipelineSpec.Tasks {
		spec.PipelineSpec.Tasks[i].RunAfter = append(task.RunAfter, startTaskName)
		finishTask.RunAfter = append(finishTask.RunAfter, task.Name)
	}

	spec.PipelineSpec.Tasks = append(spec.PipelineSpec.Tasks, startTask)
	spec.PipelineSpec.Tasks = append(spec.PipelineSpec.Tasks, finishTask)

	return nil
}
