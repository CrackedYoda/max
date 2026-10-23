package chains

import "errors"

var ErrUnsupportedNamespace = errors.New("unsupported chain namespace")

func NormalizeAddress(ns Namespace, addr string) (string, error) {
	switch ns {
	case NamespaceEVM:
		return NormalizeEVMAddress(addr)
	case NamespaceSolana:
		return NormalizeSolanaAddress(addr)
	}
	return "", ErrUnsupportedNamespace
}

func VerifySignature(ns Namespace, addr, message, signature string) error {
	switch ns {
	case NamespaceEVM:
		return VerifyEVMPersonalSign(addr, message, signature)
	case NamespaceSolana:
		return VerifySolanaSignature(addr, message, signature)
	}
	return ErrUnsupportedNamespace
}

// DisplayAddress renders a canonical address in its conventional display form.
func DisplayAddress(ns Namespace, addr string) string {
	if ns == NamespaceEVM {
		return ChecksumEVMAddress(addr)
	}
	return addr
}
