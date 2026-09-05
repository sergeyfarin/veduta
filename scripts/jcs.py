"""Canonical manifest digest: RFC 8785 (JSON Canonicalization Scheme), restricted subset.

Mirrors scripts/gocheck/canonical.go exactly; testdata/canonical/ holds golden digests that both
implementations must reproduce. "Sorted-key JSON" was never a specification: Python and Go differ
on Unicode escaping, on <>& escaping, and on float formatting.
"""
import hashlib

# JCS interoperability is defined over IEEE 754 doubles, so an integer outside this range
# canonicalises differently depending on the reader. Both implementations reject rather than
# silently disagree. Mirrors minSafeInt/maxSafeInt in gocheck.
MAX_SAFE_INT = 2 ** 53 - 1
MIN_SAFE_INT = -(2 ** 53 - 1)

# EXACT locations whose arrays are sorted and de-duplicated before hashing. Normalising by
# property name alone was a hole: `params` accepts arbitrary JSON Schema, so a manifest could
# carry params.default.capabilities and two semantically different documents would hash alike.
# "*" matches one path element (array index or map key). Mirrors setLikePaths in gocheck.
SET_LIKE_PATHS = (
    ("spec", "capabilities"),
    ("spec", "operations", "*", "routes", "*", "queryKeys"),
)


def _is_set_like(path):
    for p in SET_LIKE_PATHS:
        if len(p) == len(path) and all(a == "*" or a == b for a, b in zip(p, path)):
            return True
    return False
ESC = {
    chr(0x22): chr(92) + chr(0x22),
    chr(92): chr(92) + chr(92),
    chr(8): chr(92) + "b",
    chr(12): chr(92) + "f",
    chr(10): chr(92) + "n",
    chr(13): chr(92) + "r",
    chr(9): chr(92) + "t",
}


def normalise(v, path=()):
    if isinstance(v, dict):
        return {k: normalise(e, path + (k,)) for k, e in v.items()}
    if isinstance(v, list):
        out = [normalise(e, path + (str(i),)) for i, e in enumerate(v)]
        if _is_set_like(path):
            if not all(isinstance(e, str) for e in out):
                raise ValueError("/%s: set-like array must contain strings" % "/".join(path))
            return sorted(set(out))
        return out
    if isinstance(v, bool):
        return v
    if isinstance(v, float):
        raise ValueError("/%s: floating-point value; manifests must use integers only"
                         % "/".join(path))
    if isinstance(v, int) and not (MIN_SAFE_INT <= v <= MAX_SAFE_INT):
        raise ValueError("/%s: integer %d outside the JSON safe range +/-(2^53-1)"
                         % ("/".join(path), v))
    return v


def esc(s):
    out = [chr(0x22)]
    for ch in s:
        if ch in ESC:
            out.append(ESC[ch])
        elif ord(ch) < 0x20:
            out.append(chr(92) + "u%04x" % ord(ch))
        else:
            out.append(ch)
    out.append(chr(0x22))
    return "".join(out)


def _utf16(s):
    return s.encode("utf-16-be")


def serialise(v):
    if v is None:
        return "null"
    if v is True:
        return "true"
    if v is False:
        return "false"
    if isinstance(v, int):
        return str(v)
    if isinstance(v, str):
        return esc(v)
    if isinstance(v, list):
        return "[" + ",".join(serialise(e) for e in v) + "]"
    if isinstance(v, dict):
        return "{" + ",".join(esc(k) + ":" + serialise(v[k]) for k in sorted(v, key=_utf16)) + "}"
    raise TypeError(type(v))


def digest(doc):
    return hashlib.sha256(serialise(normalise(doc)).encode("utf-8")).hexdigest()
