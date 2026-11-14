# Copyright 2026 colonel-byte
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# syntax=docker/dockerfile:1

# One Dockerfile for both managers, selected by MANAGER. They differ only in which binary is
# copied in, and two files that differed in one COPY line would drift.
#
# The digest is pinned, and Dependabot owns moving it -- see .github/dependabot.yaml. A floating
# tag in a controller image is how a management cluster ends up running something nobody chose.

# Chainguard static, matching the cargoship base image: no shell, no package manager, nothing to
# exec into. These controllers hold a cluster-wide SSH key in memory, which is the single most
# valuable thing in the management cluster, so the image has nothing in it that could read process
# memory or open a socket.
FROM cgr.dev/chainguard/static:latest@sha256:fc42af33e643f20d08baf03cddcc1c02ba57052dfb02edb0d79f3fc47bcea8d9

ARG TARGETPLATFORM
ARG MANAGER

# Renamed to /manager so the deployment's command is the same for both, which is what lets the two
# kustomize overlays share one manager.yaml.
COPY ${TARGETPLATFORM}/${MANAGER} /manager

# The control-plane manager extracts distro packages here, and the deployment mounts a volume over
# it. Declared so the image is honest about needing writable storage even when the mount is
# forgotten -- without it, a missing volume fails at the first pull with a read-only filesystem
# error rather than at startup.
WORKDIR /var/lib/cargoship

USER 65532:65532

ENTRYPOINT ["/manager"]
