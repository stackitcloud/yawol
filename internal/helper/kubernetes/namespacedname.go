package kubernetes

import (
	"errors"
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

var ErrNamespacedNameSplit = errors.New("unable to decode string into NamespacedName")

func ToNamespacedName(nnString string) (types.NamespacedName, error) {
	splittedNN := strings.Split(nnString, string(types.Separator))
	if len(splittedNN) != 2 {
		return types.NamespacedName{}, ErrNamespacedNameSplit
	}

	return types.NamespacedName{
		Namespace: splittedNN[0],
		Name:      splittedNN[1],
	}, nil
}
