// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package storj

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"database/sql/driver"
	"encoding/binary"
	"hash"

	"github.com/zeebo/errs"
)

// ErrPieceID is used when something goes wrong with a piece ID.
var ErrPieceID = errs.Class("piece ID")

// PieceID is the unique identifier for pieces.
type PieceID [32]byte

// NewPieceID creates a piece ID.
func NewPieceID() PieceID {
	var id PieceID

	_, err := rand.Read(id[:])
	if err != nil {
		panic(err)
	}

	return id
}

// PieceIDFromString decodes a hex encoded piece ID string.
func PieceIDFromString(s string) (PieceID, error) {
	idBytes, err := base32Encoding.DecodeString(s)
	if err != nil {
		return PieceID{}, ErrPieceID.Wrap(err)
	}
	return PieceIDFromBytes(idBytes)
}

// PieceIDFromBytes converts a byte slice into a piece ID.
func PieceIDFromBytes(b []byte) (PieceID, error) {
	if len(b) != len(PieceID{}) {
		return PieceID{}, ErrPieceID.New("not enough bytes to make a piece ID; have %d, need %d", len(b), len(PieceID{}))
	}

	var id PieceID
	copy(id[:], b)
	return id, nil
}

// IsZero returns whether piece ID is unassigned.
func (id PieceID) IsZero() bool {
	return id == PieceID{}
}

// String representation of the piece ID.
func (id PieceID) String() string { return base32Encoding.EncodeToString(id.Bytes()) }

// Bytes returns bytes of the piece ID.
func (id PieceID) Bytes() []byte { return id[:] }

// Derive a new PieceID from the current piece ID, the given storage node ID and piece number.
func (id PieceID) Derive(storagenodeID NodeID, pieceNum int32) PieceID {
	return id.Deriver().Derive(storagenodeID, pieceNum)
}

// Deriver creates piece ID dervier for multiple derive operations.
func (id PieceID) Deriver() PieceIDDeriver {
	return PieceIDDeriver{
		mac: hmac.New(sha512.New, id.Bytes()),
	}
}

// Marshal serializes a piece ID.
func (id PieceID) Marshal() ([]byte, error) {
	return id.Bytes(), nil
}

// MarshalTo serializes a piece ID into the passed byte slice.
func (id *PieceID) MarshalTo(data []byte) (n int, err error) {
	n = copy(data, id.Bytes())
	return n, nil
}

// Unmarshal deserializes a piece ID.
func (id *PieceID) Unmarshal(data []byte) error {
	var err error
	*id, err = PieceIDFromBytes(data)
	return err
}

// Size returns the length of a piece ID (implements gogo's custom type interface).
func (id *PieceID) Size() int {
	return len(id)
}

// MarshalText serializes a piece ID to a base32 string.
func (id PieceID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

// UnmarshalText deserializes a base32 string to a piece ID.
func (id *PieceID) UnmarshalText(data []byte) error {
	var err error
	*id, err = PieceIDFromString(string(data))
	if err != nil {
		return err
	}
	return nil
}

// Value set a PieceID to a database field.
func (id PieceID) Value() (driver.Value, error) {
	return id.Bytes(), nil
}

// Scan extracts a PieceID from a database field.
func (id *PieceID) Scan(src interface{}) (err error) {
	b, ok := src.([]byte)
	if !ok {
		return ErrPieceID.New("PieceID Scan expects []byte")
	}
	n, err := PieceIDFromBytes(b)
	*id = n
	return err
}

// PieceIDDeriver can be used to for multiple derivation from the same PieceID
// without need to initialize mac for each Derive call.
type PieceIDDeriver struct {
	mac hash.Hash
}

// Derive a new PieceID from the piece ID, the given storage node ID and piece number.
// Initial mac is created from piece ID once while creating PieceDeriver and just
// reset to initial state at the beginning of each call.
func (pd PieceIDDeriver) Derive(storagenodeID NodeID, pieceNum int32) PieceID {
	pd.mac.Reset()

	_, _ = pd.mac.Write(storagenodeID.Bytes()) // on hash.Hash write never returns an error
	num := make([]byte, 4)
	binary.BigEndian.PutUint32(num, uint32(pieceNum))
	_, _ = pd.mac.Write(num) // on hash.Hash write never returns an error
	var derived PieceID
	copy(derived[:], pd.mac.Sum(nil))
	return derived
}
