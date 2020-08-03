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
	"strconv"
	"testing"
	"time"

	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/diff"

	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	pipelinev1alpha1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	prowapi "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"k8s.io/test-infra/prow/kube"
)

func TestProwJobToPipelineRun(t *testing.T) {
	var sshKeyMode int32 = 0400
	tests := []struct {
		name     string
		buildID  string
		labels   map[string]string
		pjSpec   prowapi.ProwJobSpec
		pjStatus prowapi.ProwJobStatus
		expected *pipelinev1alpha1.PipelineRun
	}{
		{
			name:    "pipeline",
			buildID: "blabla",
			labels:  map[string]string{"needstobe": "inherited"},
			pjSpec: prowapi.ProwJobSpec{
				Type: prowapi.PresubmitJob,
				Job:  "job-name",
				DecorationConfig: &prowapi.DecorationConfig{
					Timeout:     &prowapi.Duration{Duration: 120 * time.Minute},
					GracePeriod: &prowapi.Duration{Duration: 10 * time.Second},
					UtilityImages: &prowapi.UtilityImages{
						CloneRefs:  "clonerefs:tag",
						InitUpload: "initupload:tag",
						Entrypoint: "entrypoint:tag",
						Sidecar:    "sidecar:tag",
					},
					GCSConfiguration: &prowapi.GCSConfiguration{
						Bucket:       "my-bucket",
						PathStrategy: "legacy",
						DefaultOrg:   "kubernetes",
						DefaultRepo:  "kubernetes",
					},
					GCSCredentialsSecret: "secret-name",
					SSHKeySecrets:        []string{"ssh-1", "ssh-2"},
					CookiefileSecret:     "yummy/.gitcookies",
				},
				Agent: prowapi.TektonAgent,
				Refs: &prowapi.Refs{
					Org:     "org-name",
					Repo:    "repo-name",
					BaseRef: "base-ref",
					BaseSHA: "base-sha",
					Pulls: []prowapi.Pull{{
						Number: 1,
						Author: "author-name",
						SHA:    "pull-sha",
					}},
				},
				PipelineRunSpec: &pipelinev1alpha1.PipelineRunSpec{
					PipelineSpec: &pipelinev1alpha1.PipelineSpec{
						Tasks: []pipelinev1alpha1.PipelineTask{
							{
								Name: "task 1",
								TaskSpec: &pipelinev1alpha1.TaskSpec{
									TaskSpec: pipelinev1beta1.TaskSpec{
										Steps: []pipelinev1alpha1.Step{
											{
												Container: coreapi.Container{
													Name:    "step-1",
													Image:   "tester-1",
													Command: []string{"/bin/thing"},
													Args:    []string{"some", "args"},
													Env: []coreapi.EnvVar{
														{Name: "MY_ENV", Value: "rocks"},
													},
												},
												Script: "",
											},
											{
												Container: coreapi.Container{
													Name:    "step-2",
													Image:   "tester-2",
													Command: []string{"/bin/otherthing"},
													Args:    []string{"other", "args"},
													Env: []coreapi.EnvVar{
														{Name: "MY_ENV", Value: "rocks"},
													},
												},
												Script: "",
											},
										},
									},
								},
							},
							{
								Name: "task 2",
								TaskSpec: &pipelinev1alpha1.TaskSpec{
									TaskSpec: pipelinev1beta1.TaskSpec{
										Steps: []pipelinev1alpha1.Step{
											{
												Container: coreapi.Container{
													Name:    "step-3",
													Image:   "tester-3",
													Command: []string{"/bin/anotherthing"},
													Args:    []string{"more", "args"},
													Env: []coreapi.EnvVar{
														{Name: "MY_ENV", Value: "rocks"},
													},
												},
												Script: "",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: &pipelinev1alpha1.PipelineRun{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pipeline",
					Labels: map[string]string{
						kube.CreatedByProw:     "true",
						kube.ProwJobTypeLabel:  "presubmit",
						kube.ProwJobIDLabel:    "pipeline",
						"needstobe":            "inherited",
						kube.OrgLabel:          "org-name",
						kube.RepoLabel:         "repo-name",
						kube.PullLabel:         "1",
						kube.ProwJobAnnotation: "job-name",
						kube.ProwBuildIDLabel:  "",
					},
					Annotations: map[string]string{
						kube.ProwJobAnnotation: "job-name",
					},
				},
				Spec: pipelinev1alpha1.PipelineRunSpec{
					PipelineSpec: &pipelinev1alpha1.PipelineSpec{
						Tasks: []pipelinev1alpha1.PipelineTask{
							{
								Name:       "task 1",
								Workspaces: []pipelinev1alpha1.WorkspacePipelineTaskBinding{{Name: "code", Workspace: "code"}},
								RunAfter:   []string{"start-task"},
								TaskSpec: &pipelinev1alpha1.TaskSpec{
									TaskSpec: pipelinev1beta1.TaskSpec{
										Workspaces: []pipelinev1alpha1.WorkspaceDeclaration{{Name: "code", MountPath: "/home/prow/go"}},
										Steps: []pipelinev1alpha1.Step{
											{
												Container: coreapi.Container{
													Name:       "step-1",
													Image:      "tester-1",
													Command:    []string{"/tools/entrypoint"},
													WorkingDir: "/home/prow/go/src/github.com/org-name/repo-name",
													Env: []coreapi.EnvVar{
														{Name: "MY_ENV", Value: "rocks"},
														{Name: "ENTRYPOINT_OPTIONS", Value: `{"timeout":7200000000000,"grace_period":10000000000,"artifact_dir":"/logs/artifacts","always_zero":true,"args":["/bin/thing","some","args"],"container_name":"step-1","process_log":"/logs/step-1-log.txt","marker_file":"/logs/step-1-marker.txt","metadata_file":"/logs/artifacts/step-1-metadata.json"}`},
													},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "logs",
															MountPath: "/logs",
														},
														{
															Name:      "tools",
															MountPath: "/tools",
														},
													},
												},
												Script: "",
											},
											{
												Container: coreapi.Container{
													Name:       "step-2",
													Image:      "tester-2",
													Command:    []string{"/tools/entrypoint"},
													WorkingDir: "/home/prow/go/src/github.com/org-name/repo-name",
													Env: []coreapi.EnvVar{
														{Name: "MY_ENV", Value: "rocks"},
														{Name: "ENTRYPOINT_OPTIONS", Value: `{"timeout":7200000000000,"grace_period":10000000000,"artifact_dir":"/logs/artifacts","previous_marker":"/logs/step-1-marker.txt","always_zero":true,"args":["/bin/otherthing","other","args"],"container_name":"step-2","process_log":"/logs/step-2-log.txt","marker_file":"/logs/step-2-marker.txt","metadata_file":"/logs/artifacts/step-2-metadata.json"}`},
													},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "logs",
															MountPath: "/logs",
														},
														{
															Name:      "tools",
															MountPath: "/tools",
														},
													},
												},
												Script: "",
											},
											{
												Container: coreapi.Container{
													Name:    "sidecar",
													Image:   "sidecar:tag",
													Command: []string{"/sidecar"},
													Env: []coreapi.EnvVar{
														{Name: "JOB_SPEC", Value: `{"type":"presubmit","job":"job-name","buildid":"blabla","prowjobid":"pipeline","refs":{"org":"org-name","repo":"repo-name","base_ref":"base-ref","base_sha":"base-sha","pulls":[{"number":1,"author":"author-name","sha":"pull-sha"}]},"decoration_config":{"timeout":"2h0m0s","grace_period":"10s","utility_images":{"clonerefs":"clonerefs:tag","initupload":"initupload:tag","entrypoint":"entrypoint:tag","sidecar":"sidecar:tag"},"gcs_configuration":{"bucket":"my-bucket","path_strategy":"legacy","default_org":"kubernetes","default_repo":"kubernetes"},"gcs_credentials_secret":"secret-name","ssh_key_secrets":["ssh-1","ssh-2"],"cookiefile_secret":"yummy/.gitcookies"}}`},
														{Name: "SIDECAR_OPTIONS", Value: `{"gcs_options":{"items":["/logs/artifacts"],"bucket":"my-bucket","path_strategy":"legacy","default_org":"kubernetes","default_repo":"kubernetes","gcs_credentials_file":"/secrets/gcs/service-account.json","dry_run":false},"entries":[{"args":["/bin/thing","some","args"],"container_name":"step-1","process_log":"/logs/step-1-log.txt","marker_file":"/logs/step-1-marker.txt","metadata_file":"/logs/artifacts/step-1-metadata.json"},{"args":["/bin/otherthing","other","args"],"container_name":"step-2","process_log":"/logs/step-2-log.txt","marker_file":"/logs/step-2-marker.txt","metadata_file":"/logs/artifacts/step-2-metadata.json"}]}`},
													},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "logs",
															MountPath: "/logs",
														},
														{
															Name:      "gcs-credentials",
															MountPath: "/secrets/gcs",
														},
													},
												},
												Script: "",
											},
										},
									},
								},
							},
							{
								Name:       "task 2",
								Workspaces: []pipelinev1alpha1.WorkspacePipelineTaskBinding{{Name: "code", Workspace: "code"}},
								RunAfter:   []string{"start-task"},
								TaskSpec: &pipelinev1alpha1.TaskSpec{
									TaskSpec: pipelinev1beta1.TaskSpec{
										Workspaces: []pipelinev1alpha1.WorkspaceDeclaration{{Name: "code", MountPath: "/home/prow/go"}},
										Steps: []pipelinev1alpha1.Step{
											{
												Container: coreapi.Container{
													Name:       "step-3",
													Image:      "tester-3",
													Command:    []string{"/tools/entrypoint"},
													WorkingDir: "/home/prow/go/src/github.com/org-name/repo-name",
													Env: []coreapi.EnvVar{
														{Name: "MY_ENV", Value: "rocks"},
														{Name: "ENTRYPOINT_OPTIONS", Value: `{"timeout":7200000000000,"grace_period":10000000000,"artifact_dir":"/logs/artifacts","always_zero":true,"args":["/bin/anotherthing","more","args"],"container_name":"step-3","process_log":"/logs/step-3-log.txt","marker_file":"/logs/step-3-marker.txt","metadata_file":"/logs/artifacts/step-3-metadata.json"}`},
													},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "logs",
															MountPath: "/logs",
														},
														{
															Name:      "tools",
															MountPath: "/tools",
														},
													},
												},
												Script: "",
											},
											{
												Container: coreapi.Container{
													Name:    "sidecar",
													Image:   "sidecar:tag",
													Command: []string{"/sidecar"},
													Env: []coreapi.EnvVar{
														{Name: "JOB_SPEC", Value: `{"type":"presubmit","job":"job-name","buildid":"blabla","prowjobid":"pipeline","refs":{"org":"org-name","repo":"repo-name","base_ref":"base-ref","base_sha":"base-sha","pulls":[{"number":1,"author":"author-name","sha":"pull-sha"}]},"decoration_config":{"timeout":"2h0m0s","grace_period":"10s","utility_images":{"clonerefs":"clonerefs:tag","initupload":"initupload:tag","entrypoint":"entrypoint:tag","sidecar":"sidecar:tag"},"gcs_configuration":{"bucket":"my-bucket","path_strategy":"legacy","default_org":"kubernetes","default_repo":"kubernetes"},"gcs_credentials_secret":"secret-name","ssh_key_secrets":["ssh-1","ssh-2"],"cookiefile_secret":"yummy/.gitcookies"}}`},
														{Name: "SIDECAR_OPTIONS", Value: `{"gcs_options":{"items":["/logs/artifacts"],"bucket":"my-bucket","path_strategy":"legacy","default_org":"kubernetes","default_repo":"kubernetes","gcs_credentials_file":"/secrets/gcs/service-account.json","dry_run":false},"entries":[{"args":["/bin/anotherthing","more","args"],"container_name":"step-3","process_log":"/logs/step-3-log.txt","marker_file":"/logs/step-3-marker.txt","metadata_file":"/logs/artifacts/step-3-metadata.json"}]}`},
													},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "logs",
															MountPath: "/logs",
														},
														{
															Name:      "gcs-credentials",
															MountPath: "/secrets/gcs",
														},
													},
												},
												Script: "",
											},
										},
									},
								},
							},
							{
								Name:       "start-task",
								Workspaces: []pipelinev1alpha1.WorkspacePipelineTaskBinding{{Name: "code", Workspace: "code"}},
								TaskSpec: &v1alpha1.TaskSpec{
									TaskSpec: v1beta1.TaskSpec{
										Steps: []v1beta1.Step{
											{
												Container: coreapi.Container{
													Name:    "clonerefs",
													Image:   "clonerefs:tag",
													Args:    []string{"--cookiefile=" + cookiePathOnly("yummy/.gitcookies")},
													Command: []string{"/clonerefs"},
													Env: []coreapi.EnvVar{
														{Name: "CLONEREFS_OPTIONS", Value: `{"src_root":"/home/prow/go","log":"/logs/clone.json","git_user_name":"ci-robot","git_user_email":"ci-robot@k8s.io","refs":[{"org":"org-name","repo":"repo-name","base_ref":"base-ref","base_sha":"base-sha","pulls":[{"number":1,"author":"author-name","sha":"pull-sha"}]}],"key_files":["/secrets/ssh/ssh-1","/secrets/ssh/ssh-2"],"cookie_path":"` + cookiePathOnly("yummy/.gitcookies") + `"}`},
													},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "logs",
															MountPath: "/logs",
														},
														{
															Name:      "ssh-keys-ssh-1",
															MountPath: "/secrets/ssh/ssh-1",
															ReadOnly:  true,
														},
														{
															Name:      "ssh-keys-ssh-2",
															MountPath: "/secrets/ssh/ssh-2",
															ReadOnly:  true,
														},
														{
															Name:      "clonerefs-tmp",
															MountPath: "/tmp",
														},
														cookieMountOnly("yummy/.gitcookies"),
													},
												},
											},
											{
												Container: coreapi.Container{
													Name:    "initupload",
													Image:   "initupload:tag",
													Command: []string{"/initupload"},
													Env: []coreapi.EnvVar{
														{
															Name:  "INITUPLOAD_OPTIONS",
															Value: `{"bucket":"my-bucket","path_strategy":"legacy","default_org":"kubernetes","default_repo":"kubernetes","gcs_credentials_file":"/secrets/gcs/service-account.json","dry_run":false,"log":"/logs/clone.json"}`,
														},
														{
															Name:  "JOB_SPEC",
															Value: `{"type":"presubmit","job":"job-name","buildid":"blabla","prowjobid":"pipeline","refs":{"org":"org-name","repo":"repo-name","base_ref":"base-ref","base_sha":"base-sha","pulls":[{"number":1,"author":"author-name","sha":"pull-sha"}]},"decoration_config":{"timeout":"2h0m0s","grace_period":"10s","utility_images":{"clonerefs":"clonerefs:tag","initupload":"initupload:tag","entrypoint":"entrypoint:tag","sidecar":"sidecar:tag"},"gcs_configuration":{"bucket":"my-bucket","path_strategy":"legacy","default_org":"kubernetes","default_repo":"kubernetes"},"gcs_credentials_secret":"secret-name","ssh_key_secrets":["ssh-1","ssh-2"],"cookiefile_secret":"yummy/.gitcookies"}}`,
														},
													},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "logs",
															MountPath: "/logs",
														},
														{
															Name:      "gcs-credentials",
															MountPath: "/secrets/gcs",
														},
													},
												},
											},
											{
												Container: coreapi.Container{
													Name:    "place-entrypoint",
													Image:   "entrypoint:tag",
													Command: []string{"/bin/cp"},
													Args:    []string{"/entrypoint", "/tools/entrypoint"},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "tools",
															MountPath: "/tools",
														},
													},
												},
											},
										},
										Volumes: []coreapi.Volume{
											{
												Name: "ssh-keys-ssh-1",
												VolumeSource: coreapi.VolumeSource{
													Secret: &coreapi.SecretVolumeSource{
														SecretName:  "ssh-1",
														DefaultMode: &sshKeyMode,
													},
												},
											},
											{
												Name: "ssh-keys-ssh-2",
												VolumeSource: coreapi.VolumeSource{
													Secret: &coreapi.SecretVolumeSource{
														SecretName:  "ssh-2",
														DefaultMode: &sshKeyMode,
													},
												},
											},
											{
												Name: "clonerefs-tmp",
												VolumeSource: coreapi.VolumeSource{
													EmptyDir: &coreapi.EmptyDirVolumeSource{},
												},
											},
											cookieVolumeOnly("yummy"),
										},
									},
								},
							},
							{
								Name: "finish-task",
								TaskSpec: &v1alpha1.TaskSpec{
									TaskSpec: v1beta1.TaskSpec{
										Steps: []v1beta1.Step{
											{
												Container: coreapi.Container{
													Name:    "sidecar",
													Image:   "sidecar:tag",
													Command: []string{"/sidecar"},
													Env: []coreapi.EnvVar{
														{Name: "JOB_SPEC", Value: `{"type":"presubmit","job":"job-name","buildid":"blabla","prowjobid":"pipeline","refs":{"org":"org-name","repo":"repo-name","base_ref":"base-ref","base_sha":"base-sha","pulls":[{"number":1,"author":"author-name","sha":"pull-sha"}]},"decoration_config":{"timeout":"2h0m0s","grace_period":"10s","utility_images":{"clonerefs":"clonerefs:tag","initupload":"initupload:tag","entrypoint":"entrypoint:tag","sidecar":"sidecar:tag"},"gcs_configuration":{"bucket":"my-bucket","path_strategy":"legacy","default_org":"kubernetes","default_repo":"kubernetes"},"gcs_credentials_secret":"secret-name","ssh_key_secrets":["ssh-1","ssh-2"],"cookiefile_secret":"yummy/.gitcookies"}}`},
														{Name: "SIDECAR_OPTIONS", Value: `{"gcs_options":{"items":["/logs/artifacts"],"bucket":"my-bucket","path_strategy":"legacy","default_org":"kubernetes","default_repo":"kubernetes","gcs_credentials_file":"/secrets/gcs/service-account.json","dry_run":false}}`},
													},
													VolumeMounts: []coreapi.VolumeMount{
														{
															Name:      "logs",
															MountPath: "/logs",
														},
														{
															Name:      "gcs-credentials",
															MountPath: "/secrets/gcs",
														},
													},
												},
											},
										},
									},
								},
								RunAfter: []string{"task 1", "task 2"},
							},
						},
					},
					Workspaces: []v1beta1.WorkspaceBinding{
						{
							Name: "code",
							VolumeClaimTemplate: &coreapi.PersistentVolumeClaim{
								Spec: coreapi.PersistentVolumeClaimSpec{
									AccessModes: []coreapi.PersistentVolumeAccessMode{"ReadWriteOnce"},
									Resources: coreapi.ResourceRequirements{
										Requests: coreapi.ResourceList{
											coreapi.ResourceStorage: resource.MustParse("5Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			pj := prowapi.ProwJob{ObjectMeta: metav1.ObjectMeta{Name: test.name, Labels: test.labels}, Spec: test.pjSpec, Status: test.pjStatus}
			got, err := ProwJobToPipelineRun(pj, test.buildID)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !equality.Semantic.DeepEqual(got, test.expected) {
				t.Errorf("unexpected pod diff:\n%s", diff.ObjectReflectDiff(test.expected, got))
			}
		})
	}
}
