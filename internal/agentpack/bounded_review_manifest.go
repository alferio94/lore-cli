package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
)

// BoundedReviewManifest is release metadata for the static pair. It is kept in
// agentpack so a later Pi-only installer can validate it before planning work.
type BoundedReviewManifest struct {
	Family                string                      `json:"family"`
	ProjectionID          string                      `json:"projection_id"`
	ContractVersion       string                      `json:"contract_version"`
	Harness               string                      `json:"harness"`
	ResultSchemaVersion   string                      `json:"result_schema_version"`
	StaticContractVersion string                      `json:"static_contract_version"`
	StaticContractDigest  string                      `json:"static_contract_digest"`
	ManifestRevision      int                         `json:"manifest_revision"`
	Roles                 []BoundedReviewManifestRole `json:"roles"`
}

// BoundedReviewManifestRole binds one static role marker and body hash to its
// runtime-compatible handshake. Body hashes support future file integrity only.
type BoundedReviewManifestRole struct {
	Role       string                 `json:"role"`
	Marker     string                 `json:"marker"`
	BodySHA256 string                 `json:"body_sha256"`
	Handshake  BoundedReviewHandshake `json:"handshake"`
}

func NewBoundedReviewManifest(revision int, judge, fix BoundedReviewAsset) BoundedReviewManifest {
	return BoundedReviewManifest{
		Family:                BoundedReviewFamily,
		ProjectionID:          BoundedReviewProjectionID,
		ContractVersion:       BoundedReviewContractVersion,
		Harness:               BoundedReviewTarget,
		ResultSchemaVersion:   BoundedReviewResultSchemaVersion,
		StaticContractVersion: BoundedReviewStaticContractVersion,
		StaticContractDigest:  BoundedReviewStaticContractDigest(),
		ManifestRevision:      revision,
		Roles: []BoundedReviewManifestRole{
			boundedReviewManifestRole(judge),
			boundedReviewManifestRole(fix),
		},
	}
}

func boundedReviewManifestRole(asset BoundedReviewAsset) BoundedReviewManifestRole {
	return BoundedReviewManifestRole{
		Role:       asset.Role,
		Marker:     asset.Marker,
		BodySHA256: boundedReviewBodySHA256(asset.Body),
		Handshake:  asset.Handshake,
	}
}

func boundedReviewBodySHA256(body string) string {
	digest := sha256.Sum256([]byte(body))
	return hex.EncodeToString(digest[:])
}

// Validate ensures metadata is complete, ordered, and exactly bound to its
// assets. No filesystem or activation behavior belongs here.
func (manifest BoundedReviewManifest) Validate(pair BoundedReviewContractPair) error {
	if manifest.Family != BoundedReviewFamily || manifest.ProjectionID != BoundedReviewProjectionID || manifest.ContractVersion != BoundedReviewContractVersion || manifest.Harness != BoundedReviewTarget || manifest.ResultSchemaVersion != BoundedReviewResultSchemaVersion || manifest.StaticContractVersion != BoundedReviewStaticContractVersion || manifest.StaticContractDigest != BoundedReviewStaticContractDigest() || manifest.ManifestRevision != pair.Revision {
		return fmt.Errorf("static manifest identity mismatch")
	}
	if len(manifest.Roles) != 2 {
		return fmt.Errorf("manifest must contain exactly judge and fix roles")
	}
	assets := []BoundedReviewAsset{pair.Judge, pair.Fix}
	for index, role := range manifest.Roles {
		asset := assets[index]
		if role.Role != asset.Role || role.Marker != asset.Marker || role.BodySHA256 != boundedReviewBodySHA256(asset.Body) || !reflect.DeepEqual(role.Handshake, asset.Handshake) {
			return fmt.Errorf("role %d does not bind its asset", index)
		}
	}
	if manifest.Roles[0].Role != BoundedReviewJudgeRole || manifest.Roles[1].Role != BoundedReviewFixRole {
		return fmt.Errorf("manifest roles must be ordered judge then fix")
	}
	return nil
}
