// Copyright 2020 ConsenSys Software Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by gnark DO NOT EDIT

package plonk

import (
	"crypto/sha256"
	"errors"
	"io"
	"math/big"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr/kzg"

	curve "github.com/consensys/gnark-crypto/ecc/bn254"

	"text/template"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/fiat-shamir"
	"github.com/consensys/gnark/logger"
)

var (
	errWrongClaimedQuotient = errors.New("claimed quotient is not as expected")
)

func Verify(proof *Proof, vk *VerifyingKey, publicWitness fr.Vector) error {
	log := logger.Logger().With().Str("curve", "bn254").Str("backend", "plonk").Logger()
	start := time.Now()

	// pick a hash function to derive the challenge (the same as in the prover)
	hFunc := sha256.New()

	// transcript to derive the challenge
	fs := fiatshamir.NewTranscript(hFunc, "gamma", "beta", "alpha", "zeta")

	// The first challenge is derived using the public data: the commitments to the permutation,
	// the coefficients of the circuit, and the public inputs.
	// derive gamma from the Comm(blinded cl), Comm(blinded cr), Comm(blinded co)
	if err := bindPublicData(&fs, "gamma", *vk, publicWitness, proof.Bsb22Commitments); err != nil {
		return err
	}
	gamma, err := deriveRandomness(&fs, "gamma", &proof.LRO[0], &proof.LRO[1], &proof.LRO[2])
	if err != nil {
		return err
	}

	// derive beta from Comm(l), Comm(r), Comm(o)
	beta, err := deriveRandomness(&fs, "beta")
	if err != nil {
		return err
	}

	// derive alpha from Comm(l), Comm(r), Comm(o), Com(Z)
	alpha, err := deriveRandomness(&fs, "alpha", &proof.Z)
	if err != nil {
		return err
	}

	// derive zeta, the point of evaluation
	zeta, err := deriveRandomness(&fs, "zeta", &proof.H[0], &proof.H[1], &proof.H[2])
	if err != nil {
		return err
	}

	// evaluation of Z=Xⁿ⁻¹ at ζ
	var zetaPowerM, zzeta fr.Element
	var bExpo big.Int
	one := fr.One()
	bExpo.SetUint64(vk.Size)
	zetaPowerM.Exp(zeta, &bExpo)
	zzeta.Sub(&zetaPowerM, &one)

	// compute PI = ∑_{i<n} Lᵢ*wᵢ
	// TODO use batch inversion
	var pi, lagrangeOne fr.Element
	{
		var den, xiLi fr.Element
		lagrange := zzeta // ζⁿ⁻¹
		wPowI := fr.One()
		den.Sub(&zeta, &wPowI)
		lagrange.Div(&lagrange, &den).Mul(&lagrange, &vk.SizeInv) // (1/n)(ζⁿ-1)/(ζ-1)
		lagrangeOne.Set(&lagrange)                                // save it for later
		for i := 0; i < len(publicWitness); i++ {

			xiLi.Mul(&lagrange, &publicWitness[i])
			pi.Add(&pi, &xiLi)

			// use Lᵢ₊₁ = w×Lᵢ(ζ-wⁱ)/(ζ-wⁱ⁺¹)
			if i+1 != len(publicWitness) {
				lagrange.Mul(&lagrange, &vk.Generator).
					Mul(&lagrange, &den)
				wPowI.Mul(&wPowI, &vk.Generator)
				den.Sub(&zeta, &wPowI)
				lagrange.Div(&lagrange, &den)
			}
		}

		for i := range vk.CommitmentConstraintIndexes {
			var hashRes []fr.Element
			if hashRes, err = fr.Hash(proof.Bsb22Commitments[i].Marshal(), []byte("BSB22-Plonk"), 1); err != nil {
				return err
			}

			// Computing L_{CommitmentIndex}

			wPowI.Exp(vk.Generator, big.NewInt(int64(vk.NbPublicVariables)+int64(vk.CommitmentConstraintIndexes[i])))
			den.Sub(&zeta, &wPowI) // ζ-wⁱ

			lagrange.SetOne().
				Sub(&zeta, &lagrange).       // ζ-1
				Mul(&lagrange, &wPowI).      // wⁱ(ζ-1)
				Div(&lagrange, &den).        // wⁱ(ζ-1)/(ζ-wⁱ)
				Mul(&lagrange, &lagrangeOne) // wⁱ/n (ζⁿ-1)/(ζ-wⁱ)

			xiLi.Mul(&lagrange, &hashRes[0])
			pi.Add(&pi, &xiLi)
		}
	}

	// linearizedpolynomial + pi(ζ) + α*(Z(μζ))*(l(ζ)+β*s1(ζ)+γ)*(r(ζ)+β*s2(ζ)+γ)*(o(ζ)+γ) - α²*L₁(ζ)
	var _s1, _s2, _o, alphaSquareLagrange fr.Element

	zu := proof.ZShiftedOpening.ClaimedValue

	claimedQuotient := proof.BatchedProof.ClaimedValues[0]
	linearizedPolynomialZeta := proof.BatchedProof.ClaimedValues[1]
	l := proof.BatchedProof.ClaimedValues[2]
	r := proof.BatchedProof.ClaimedValues[3]
	o := proof.BatchedProof.ClaimedValues[4]
	s1 := proof.BatchedProof.ClaimedValues[5]
	s2 := proof.BatchedProof.ClaimedValues[6]

	_s1.Mul(&s1, &beta).Add(&_s1, &l).Add(&_s1, &gamma) // (l(ζ)+β*s1(ζ)+γ)
	_s2.Mul(&s2, &beta).Add(&_s2, &r).Add(&_s2, &gamma) // (r(ζ)+β*s2(ζ)+γ)
	_o.Add(&o, &gamma)                                  // (o(ζ)+γ)

	_s1.Mul(&_s1, &_s2).
		Mul(&_s1, &_o).
		Mul(&_s1, &alpha).
		Mul(&_s1, &zu) //  α*(Z(μζ))*(l(ζ)+β*s1(ζ)+γ)*(r(ζ)+β*s2(ζ)+γ)*(o(ζ)+γ)

	alphaSquareLagrange.Mul(&lagrangeOne, &alpha).
		Mul(&alphaSquareLagrange, &alpha) // α²*L₁(ζ)

	linearizedPolynomialZeta.
		Add(&linearizedPolynomialZeta, &pi).                 // linearizedpolynomial + pi(zeta)
		Add(&linearizedPolynomialZeta, &_s1).                // linearizedpolynomial+pi(zeta)+α*(Z(μζ))*(l(ζ)+s1(ζ)+γ)*(r(ζ)+s2(ζ)+γ)*(o(ζ)+γ)
		Sub(&linearizedPolynomialZeta, &alphaSquareLagrange) // linearizedpolynomial+pi(zeta)+α*(Z(μζ))*(l(ζ)+s1(ζ)+γ)*(r(ζ)+s2(ζ)+γ)*(o(ζ)+γ)-α²*L₁(ζ)

	// Compute H(ζ) using the previous result: H(ζ) = prev_result/(ζⁿ-1)
	var zetaPowerMMinusOne fr.Element
	zetaPowerMMinusOne.Sub(&zetaPowerM, &one)
	linearizedPolynomialZeta.Div(&linearizedPolynomialZeta, &zetaPowerMMinusOne)

	// check that H(ζ) is as claimed
	if !claimedQuotient.Equal(&linearizedPolynomialZeta) {
		return errWrongClaimedQuotient
	}

	// compute the folded commitment to H: Comm(h₁) + ζᵐ⁺²*Comm(h₂) + ζ²⁽ᵐ⁺²⁾*Comm(h₃)
	mPlusTwo := big.NewInt(int64(vk.Size) + 2)
	var zetaMPlusTwo fr.Element
	zetaMPlusTwo.Exp(zeta, mPlusTwo)
	var zetaMPlusTwoBigInt big.Int
	zetaMPlusTwo.BigInt(&zetaMPlusTwoBigInt)
	foldedH := proof.H[2]
	foldedH.ScalarMultiplication(&foldedH, &zetaMPlusTwoBigInt)
	foldedH.Add(&foldedH, &proof.H[1])
	foldedH.ScalarMultiplication(&foldedH, &zetaMPlusTwoBigInt)
	foldedH.Add(&foldedH, &proof.H[0])

	// Compute the commitment to the linearized polynomial
	// linearizedPolynomialDigest =
	// 		l(ζ)*ql+r(ζ)*qr+r(ζ)l(ζ)*qm+o(ζ)*qo+qk+Σᵢqc'ᵢ(ζ)*BsbCommitmentᵢ +
	// 		α*( Z(μζ)(l(ζ)+β*s₁(ζ)+γ)*(r(ζ)+β*s₂(ζ)+γ)*s₃(X)-Z(X)(l(ζ)+β*id_1(ζ)+γ)*(r(ζ)+β*id_2(ζ)+γ)*(o(ζ)+β*id_3(ζ)+γ) ) +
	// 		α²*L₁(ζ)*Z
	// first part: individual constraints
	var rl fr.Element
	rl.Mul(&l, &r)

	var linearizedPolynomialDigest curve.G1Affine

	// second part: α*( Z(μζ)(l(ζ)+β*s₁(ζ)+γ)*(r(ζ)+β*s₂(ζ)+γ)*β*s₃(X)-Z(X)(l(ζ)+β*id_1(ζ)+γ)*(r(ζ)+β*id_2(ζ)+γ)*(o(ζ)+β*id_3(ζ)+γ) ) )

	var u, v, w, cosetsquare fr.Element
	u.Mul(&zu, &beta)
	v.Mul(&beta, &s1).Add(&v, &l).Add(&v, &gamma)
	w.Mul(&beta, &s2).Add(&w, &r).Add(&w, &gamma)
	_s1.Mul(&u, &v).Mul(&_s1, &w).Mul(&_s1, &alpha) // α*Z(μζ)(l(ζ)+β*s₁(ζ)+γ)*(r(ζ)+β*s₂(ζ)+γ)*β

	cosetsquare.Square(&vk.CosetShift)
	u.Mul(&beta, &zeta).Add(&u, &l).Add(&u, &gamma)                         // (l(ζ)+β*ζ+γ)
	v.Mul(&beta, &zeta).Mul(&v, &vk.CosetShift).Add(&v, &r).Add(&v, &gamma) // (r(ζ)+β*μ*ζ+γ)
	w.Mul(&beta, &zeta).Mul(&w, &cosetsquare).Add(&w, &o).Add(&w, &gamma)   // (o(ζ)+β*μ²*ζ+γ)
	_s2.Mul(&u, &v).Mul(&_s2, &w).Neg(&_s2)                                 // -(l(ζ)+β*ζ+γ)*(r(ζ)+β*u*ζ+γ)*(o(ζ)+β*u²*ζ+γ)

	// note since third part =  α²*L₁(ζ)*Z
	_s2.Mul(&_s2, &alpha).Add(&_s2, &alphaSquareLagrange) // -α*(l(ζ)+β*ζ+γ)*(r(ζ)+β*u*ζ+γ)*(o(ζ)+β*u²*ζ+γ) + α²*L₁(ζ)

	points := append(proof.Bsb22Commitments,
		vk.Ql, vk.Qr, vk.Qm, vk.Qo, vk.Qk, // first part
		vk.S[2], proof.Z, // second & third part
	)

	qC := make([]fr.Element, len(proof.Bsb22Commitments))
	copy(qC, proof.BatchedProof.ClaimedValues[7:])
	scalars := append(qC,
		l, r, rl, o, one, /* TODO Perf @Tabaie Consider just adding Qk instead */ // first part
		_s1, _s2, // second & third part
	)
	if _, err := linearizedPolynomialDigest.MultiExp(points, scalars, ecc.MultiExpConfig{}); err != nil {
		return err
	}

	// Fold the first proof
	digestsToFold := make([]curve.G1Affine, len(vk.Qcp)+7)
	copy(digestsToFold[7:], vk.Qcp)
	digestsToFold[0] = foldedH
	digestsToFold[1] = linearizedPolynomialDigest
	digestsToFold[2] = proof.LRO[0]
	digestsToFold[3] = proof.LRO[1]
	digestsToFold[4] = proof.LRO[2]
	digestsToFold[5] = vk.S[0]
	digestsToFold[6] = vk.S[1]
	foldedProof, foldedDigest, err := kzg.FoldProof(
		digestsToFold,
		&proof.BatchedProof,
		zeta,
		hFunc,
	)
	if err != nil {
		return err
	}

	// Batch verify
	var shiftedZeta fr.Element
	shiftedZeta.Mul(&zeta, &vk.Generator)
	err = kzg.BatchVerifyMultiPoints([]kzg.Digest{
		foldedDigest,
		proof.Z,
	},
		[]kzg.OpeningProof{
			foldedProof,
			proof.ZShiftedOpening,
		},
		[]fr.Element{
			zeta,
			shiftedZeta,
		},
		vk.Kzg,
	)

	log.Debug().Dur("took", time.Since(start)).Msg("verifier done")

	return err
}

func bindPublicData(fs *fiatshamir.Transcript, challenge string, vk VerifyingKey, publicInputs []fr.Element, pi2 []kzg.Digest) error {

	// permutation
	if err := fs.Bind(challenge, vk.S[0].Marshal()); err != nil {
		return err
	}
	if err := fs.Bind(challenge, vk.S[1].Marshal()); err != nil {
		return err
	}
	if err := fs.Bind(challenge, vk.S[2].Marshal()); err != nil {
		return err
	}

	// coefficients
	if err := fs.Bind(challenge, vk.Ql.Marshal()); err != nil {
		return err
	}
	if err := fs.Bind(challenge, vk.Qr.Marshal()); err != nil {
		return err
	}
	if err := fs.Bind(challenge, vk.Qm.Marshal()); err != nil {
		return err
	}
	if err := fs.Bind(challenge, vk.Qo.Marshal()); err != nil {
		return err
	}
	if err := fs.Bind(challenge, vk.Qk.Marshal()); err != nil {
		return err
	}

	// public inputs
	for i := 0; i < len(publicInputs); i++ {
		if err := fs.Bind(challenge, publicInputs[i].Marshal()); err != nil {
			return err
		}
	}

	// bsb22 commitment
	for i := range pi2 {
		if err := fs.Bind(challenge, pi2[i].Marshal()); err != nil {
			return err
		}
	}

	return nil

}

func deriveRandomness(fs *fiatshamir.Transcript, challenge string, points ...*curve.G1Affine) (fr.Element, error) {

	var buf [curve.SizeOfG1AffineUncompressed]byte
	var r fr.Element

	for _, p := range points {
		buf = p.RawBytes()
		if err := fs.Bind(challenge, buf[:]); err != nil {
			return r, err
		}
	}

	b, err := fs.ComputeChallenge(challenge)
	if err != nil {
		return r, err
	}
	r.SetBytes(b)
	return r, nil
}

// ExportSolidity exports the verifying key to a solidity smart contract.
//
// See https://github.com/ConsenSys/gnark-tests for example usage.
//
// Code has not been audited and is provided as-is, we make no guarantees or warranties to its safety and reliability.
func (vk *VerifyingKey) ExportSolidity(w io.Writer) error {
	tmpl, err := template.New("").Parse(solidityTemplate)
	if err != nil {
		return err
	}
	return tmpl.Execute(w, vk)
}
