# Akuity Take Home Coding Challenge

Congratulations on making it to a new stage of the Akuity Engineering challenge. We are excited to see what you can do!

## Overview

This challenge was created to provide Akuity Engineering (backend) candidates
with an opportunity to demonstrate general familiarity with technologies and
tools within the cloud native ecosystem as well as deeper, more specialized
knowledge of designing and implementing APIs in the form of Kubernetes
[Custom Resource Definitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/),
along with controllers to manage the lifecycle of such resources. (Or,
alternatively, an opportunity to demonstrate acquiring these skills rapidly.) To
respect your time, the challenge includes (optional) lightweight scaffolding to
help you get started. _We wish for you to be able to focus immediately on the
core problem without getting bogged down in minutiae._

## Problem Statement

For this challenge, you will take on the role of a platform engineer that wishes
to provide developers within your organization with the capability to create
their own Kubernetes namespaces on demand, however, you also wish for all
namespaces they create to reliably comply with certain organizational standards.

### Assumptions

You may assume that developers already have direct access to applicable
clusters. i.e. They can use `kubectl`. You may further assume they lack the
necessary permissions to create namespaces directly, but _will_ be capable of
creating instances of a `Tenant` resource type, which you will define.

### Requirements

`Tenant` resources MUST have the following properties:

1. Upon creation, a new namespace with the _same name_ as the `Tenant` resource
   must be created. If the namespace already exists, that is to be treated as an
   unrecoverable error and the `Tenant`'s `Status` subresource must reflect
   that. Upon subsequent reconciliation of a `Tenant` resource, the controller
   must _continue_ to ensure the existence of the corresponding namespace. i.e.
   If it has somehow been deleted, it must be restored.

    ☝️ __Tip:__ Distinguishing between the case wherein a namespace already exists
    and should be treated as an unrecoverable error and the case wherein the
    namespace corresponding to a `Tenant` already exists _because a previous
    reconciliation attempt succeeded_ will require you to track what `Tenant`
    resource
    [_owns_](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
    the namespace in question.

    Upon deletion of a `Tenant` resource, the corresponding namespace must be
    deleted.

1. All namespaces created via a `Tenant` resource must be labeled with
   `akuity.io/tenant=true`. Each time a `Tenant` resource is reconciled, if the
   corresponding namespace is found to be missing this label, it must be
   restored.

1. The `Tenant` must provide fields that permit developers to specify
   _additional_ labels to be applied to the corresponding namespace. Each time a
   `Tenant` resource is reconciled, any desired labels found to be missing from
   the namespace must be restored. Any extraneous labels present on the
   namespace, but _not_ specified by the `Tenant` resource must be removed. If a
   developer has modified the labels specified by the `Tenant` resource, the
   controller must update namespace labels accordingly.

1. Upon creation, a
   [`NetworkPolicy`](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
   resource must be created in the namespace corresponding to the `Tenant`
   resource. The `NetworkPolicy` may be named using any convention you choose.
   The `NetworkPolicy` must apply unconditionally to _all_ `Pod`s within the
   namespace and consist of the following rules:

    1. Permit all traffic _within_ the `Tenant`'s namespace.
    1. Permit egress to destinations outside the cluster _only_ if enabled
       through a `bool` field of the `Tenant` resource having the non-default
       value `true`.
    1. Deny all traffic between the `Tenant`'s namespace and other namespaces.
   
   Upon subsequent reconciliation of a `Tenant` resource, the controller must
   _continue_ to ensure the existence of the `NetworkPolicy` resource. i.e. If
   it has somehow been deleted, it must be restored. Its rules must also be
   corrected if they have somehow been modified.

### Constraints

1. Your controller MUST be implemented in [Go](https://go.dev/) and should
   generally be as idiomatic as you are able to make it. If you use the provided
   scaffolding, passing the pre-defined lint checks will not guarantee your code
   is idiomatic, but will help you avoid many common departures from convention.

1. Your controller must utilize the
   [Kubernetes controller-runtime](https://github.com/kubernetes-sigs/controller-runtime).

1. Providing unit tests for your controller is _highly_ encouraged. Any unit
   tests you do provide MUST pass when executed.

## Getting Started

As noted, we have provided you with lightweight scaffolding to get you started.
You are not _required_ to use this scaffolding, but will likely find it helpful.
_You are permitted to modify what is provided in any way you see fit._

If you choose to use this scaffolding as a starting point, you will find it
included in the same archive file in which you received this document.

☝️ __Tip:__ You will find the scaffolding works best in a Linux or macOS
environment. If you are using Windows, it is highly recommended that you
utilize the
[Windows Subsystem for Linux (WSL 2)](https://learn.microsoft.com/en-us/windows/wsl/about).

### Tools

The scaffolding relies heavily on [Docker](https://www.docker.com/get-started/)
containers to minimize time spent setting up required tools in your local
environment. As long as [`make`](https://en.wikipedia.org/wiki/Make_(software))
and `docker` work on your system, you do not _need_ to install
[`golangci-lint`](https://golangci-lint.run/),
code generation tools, such as
[`controller-gen`](https://book.kubebuilder.io/reference/controller-gen.html),
configuration management tools, such as [`kustomize`](https://kustomize.io/),
or even the Go toolchain itself.

If you find the heavy reliance on Docker containers to be a hindrance, you are
free to modify the scaffolding to suit your needs.

It is highly recommended that you do test your code end-to-end in a local
Kubernetes cluster. You may also be asked to demonstrate your solution live or
in a pre-recorded video. Any of the following tools are useful for this purpose:

- [kind](https://kind.sigs.k8s.io/)
- [k3d](https://k3d.io/)
- [minikube](https://minikube.sigs.k8s.io/docs/)
- [OrbSack](https://orbstack.dev/) (Mac only)
- [Docker Desktop](https://www.docker.com/products/docker-desktop)

We trust that you are capable of installing one of these tools as well as
[`kubectl`](https://kubernetes.io/docs/tasks/tools/#kubectl) on your system and
using them to launch a local Kubernetes cluster.

📝 __Note:__ Many of the Kubernetes distributions listed above utilize
[Container Network Interface](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/)
plugins that _do not_ enforce network policies defined by `NetworkPolicy`
resources. Do not be concerned with this limitation if you encounter it.
Defining a `NetworkPolicy` for each `Tenant` resource lies within the scope of
this challenge, but _enforcing_ it does not.

### Conveniences

If using the provided scaffolding, you will find the following `make` targets
helpful:

- `lint`: Runs `golangci-lint` with pre-defined configuration against your code.
  This target leverages a Docker container to minimize setup.

- `test`: Runs any unit tests you provide. This target leverages a Docker
  container to minimize setup.

- `codegen`: Generates CRDs and boilerplate API code. Run this after any
  modification to the `.go` files in `api/v1alpha1/` directory. This target
  leverages a Docker container to minimize setup.

- `deploy`: Builds your controller and deploys it to whatever Kubernetes 
  cluster `kubectl` is currently configured to use. Portions of this process
  leverage a Docker container to minimize setup.

### Tips

- If this challenge pushes the limits of your Kubernetes knowledge, do not
  be discouraged. _Demonstrating an ability to learn quickly and adapt to new
  technologies is in many ways more impressive than relying only on existing
  knowledge._

- You will most likely find the
  [Kubebuilder documentation](https://book.kubebuilder.io/) and
  these
  [API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
  to be valuable resources in completing this challenge.

- It is acceptable to complete the challenge with the assistance of an AI such
  as [ChatGPT](https://openai.com/chatgpt/) or
  [GitHub Copilot](https://github.com/features/copilot). We find these to be
  valuable tools in the hands of a skilled engineer and are confident these
  tools cannot presently rise to this challenge on their own. Even with their
  assistance, we trust your solution will be uniquely _yours_.

  __But please do not consult with other humans.__

- If you choose to use the provided scaffolding, search the code for `TODO` to
  quickly find areas where this challenge requires your attention.

## Timeline

Candidates are permitted up to one full week to complete this challenge.
although the challenge is designed to be completed with no more than one day's
worth of effort.

## Deliverables

Once you have completed the challenge, please push your solution to a new,
_private_ repository on GitHub. If you have not used the provided scaffolding
or have modified it extensively, please ensure the repository's `README.md`
contains adequate documentation for building and deploying your solution.

Reach out to the team to ask their GitHub username if they have not
already provided it to you. Add the judge as a collaborator on your
private repository so they will be able to access your solution. They may share
it with other Akuity staff tasked with reviewing it.
