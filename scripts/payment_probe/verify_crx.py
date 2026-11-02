#!/usr/bin/env python3
"""Verify a downloaded CRX3 developer signature before extracting a test wallet.

Requires Python cryptography. Format reference: Chromium components/crx_file/crx3.proto.
The expected extension ID must come from the wallet's official download link.
"""
import argparse
import hashlib
import io
import json
from pathlib import Path
import struct
import zipfile
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec, padding


def varint(data, offset):
    value, shift = 0, 0
    while True:
        byte = data[offset]
        offset += 1
        value |= (byte & 127) << shift
        if byte < 128:
            return value, offset
        shift += 7


def fields(data):
    offset, result = 0, []
    while offset < len(data):
        tag, offset = varint(data, offset)
        if tag & 7 != 2:
            raise ValueError("Unexpected CRX protobuf wire type")
        length, offset = varint(data, offset)
        if offset + length > len(data):
            raise ValueError("Truncated CRX header")
        result.append((tag >> 3, data[offset:offset + length]))
        offset += length
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("crx", type=Path)
    parser.add_argument("expected_id")
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    data = args.crx.read_bytes()
    if data[:4] != b"Cr24":
        raise ValueError("Not a CRX file")
    version, length = struct.unpack("<II", data[4:12])
    if version != 3 or length > 1024 * 1024 or length + 12 >= len(data):
        raise ValueError("Unsupported CRX header")
    header = fields(data[12:12 + length])
    archive = data[12 + length:]
    signed = dict(header)[10000]
    crx_id = dict(fields(signed))[1]
    extension_id = "".join(chr(97 + int(c, 16)) for c in crx_id.hex())
    if extension_id != args.expected_id:
        raise ValueError("Extension ID does not match the official download link")
    payload = b"CRX3 SignedData\0" + struct.pack("<I", len(signed)) + signed + archive
    verified = []
    for kind, proof in header:
        if kind not in (2, 3):
            continue
        proof = dict(fields(proof))
        public_key, signature = proof[1], proof[2]
        if hashlib.sha256(public_key).digest()[:16] != crx_id:
            continue
        key = serialization.load_der_public_key(public_key)
        if kind == 2:
            key.verify(signature, payload, padding.PKCS1v15(), hashes.SHA256())
        else:
            key.verify(signature, payload, ec.ECDSA(hashes.SHA256()))
        verified.append({"proofType": kind, "publicKeySha256": hashlib.sha256(public_key).hexdigest()})
    if not verified:
        raise ValueError("No valid developer proof matching the expected ID")
    args.output.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(io.BytesIO(archive)) as bundle:
        if any(Path(name).is_absolute() or ".." in Path(name).parts for name in bundle.namelist()):
            raise ValueError("Unsafe archive path")
        if bundle.testzip() is not None:
            raise ValueError("Corrupt ZIP entry")
        bundle.extractall(args.output)
    manifest = json.loads((args.output / "manifest.json").read_text())
    report = {"extensionId": extension_id, "version": manifest["version"],
              "manifestVersion": manifest["manifest_version"], "size": len(data),
              "crxSha256": hashlib.sha256(data).hexdigest(),
              "developerSignatureVerified": True, "proofs": verified,
              "manifestKeyPresent": "key" in manifest}
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
