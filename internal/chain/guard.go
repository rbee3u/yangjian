package chain

import "fmt"

var readOnlyMethods = map[string]struct{}{
	"eth_chainId":                             {},
	"eth_blockNumber":                         {},
	"eth_getBlockByNumber":                    {},
	"eth_getBlockByHash":                      {},
	"eth_getBlockReceipts":                    {},
	"eth_getBlockTransactionCountByHash":      {},
	"eth_getBlockTransactionCountByNumber":    {},
	"eth_getCode":                             {},
	"eth_getStorageAt":                        {},
	"eth_call":                                {},
	"eth_estimateGas":                         {},
	"eth_getLogs":                             {},
	"eth_getBalance":                          {},
	"eth_getTransactionCount":                 {},
	"eth_getTransactionByHash":                {},
	"eth_getTransactionByBlockHashAndIndex":   {},
	"eth_getTransactionByBlockNumberAndIndex": {},
	"eth_getTransactionReceipt":               {},
	"eth_getProof":                            {},
	"eth_feeHistory":                          {},
	"eth_gasPrice":                            {},
	"eth_maxPriorityFeePerGas":                {},
	"debug_traceTransaction":                  {},
	"debug_traceCall":                         {},
	"trace_transaction":                       {},
	"trace_call":                              {},
	"trace_block":                             {},
	"trace_get":                               {},
	"trace_filter":                            {},
	"web3_clientVersion":                      {},
	"net_version":                             {},
}

func CheckReadOnly(method string) error {
	if _, ok := readOnlyMethods[method]; !ok {
		return fmt.Errorf("method %q is not read-only", method)
	}
	return nil
}

func ReadOnlyMethods() []string {
	methods := make([]string, 0, len(readOnlyMethods))
	for method := range readOnlyMethods {
		methods = append(methods, method)
	}
	return methods
}
