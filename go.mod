module github.com/kubeless/cronjob-trigger

go 1.12

require (
	github.com/golang/glog v1.0.0
	github.com/imdario/mergo v0.3.12
	github.com/kubeless/kubeless v1.0.3
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.4.0
	k8s.io/api v0.24.1
	k8s.io/apiextensions-apiserver v0.24.1
	k8s.io/apimachinery v0.24.1
	k8s.io/client-go v0.24.1
)

replace github.com/kubeless/kubeless => ../kubeless

replace github.com/kubeless/http-trigger => ../http-trigger
