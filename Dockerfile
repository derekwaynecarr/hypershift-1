FROM registry.svc.ci.openshift.org/openshift/release:golang-1.15 as builder

WORKDIR /hypershift

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -o bin/hypershift-operator hypershift-operator/main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -o bin/control-plane-operator control-plane-operator/main.go

FROM quay.io/openshift/origin-base:4.6
COPY --from=builder /hypershift/bin/hypershift-operator /usr/bin/hypershift-operator
COPY --from=builder /hypershift/bin/control-plane-operator /usr/bin/control-plane-operator
