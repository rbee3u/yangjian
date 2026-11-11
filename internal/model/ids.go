package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

func NormalizeAddress(address string) (string, error) {
	if !common.IsHexAddress(address) {
		return "", fmt.Errorf("invalid address %q", address)
	}
	return common.HexToAddress(address).Hex(), nil
}

func ContractID(chainID int64, address string) string {
	return fmt.Sprintf("%d:%s", chainID, strings.ToLower(common.HexToAddress(address).Hex()))
}

func ContractVersionID(chainID int64, address string, validFromBlock uint64) string {
	return fmt.Sprintf("%d:%s@%d", chainID, strings.ToLower(common.HexToAddress(address).Hex()), validFromBlock)
}

// AccountID identifies a trace endpoint that was not observed to have runtime
// code at the observed block. It keeps observed_call relations from labeling an
// EOA, a precompile, or an unclassified endpoint as a Contract.
func AccountID(chainID int64, address string) string {
	return fmt.Sprintf("account:%d:%s", chainID, strings.ToLower(common.HexToAddress(address).Hex()))
}

func SourceRevisionID(data []byte) string {
	return ContentHash(data)
}

func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func RelationID(relationType, from, to, evidenceHash string) string {
	return fmt.Sprintf("%s:%s:%s:%s", relationType, from, to, evidenceHash)
}
