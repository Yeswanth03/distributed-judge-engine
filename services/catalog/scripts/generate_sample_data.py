"""Generates src/main/resources/db/sample/R__sample_problems.sql: five published problems for local testing.

Each problem is a function to implement (method of class Solution). Cases are JSON: input holds the arguments and
expected_output the return value, in the same compact form the service stores.

Each problem gets two published test-set versions (v2 is current) of 20 cases each; the first two cases are samples.
v2 adds large cases (n up to 2·10^5, grids up to 1000 x 1000) that an O(n²) brute force cannot finish in time.
Every expected output is computed by the reference solution below, and each reference is cross-checked against a
brute force on small inputs, so the data is correct by construction. Ids are fixed (uuid5), so every teammate's
local database has the same problem ids.

Run from services/catalog:  python3 scripts/generate_sample_data.py
"""

import hashlib
import json
import random
import struct
import uuid
from collections import deque
from pathlib import Path

OUT = Path(__file__).resolve().parent.parent / "src/main/resources/db/sample/R__sample_problems.sql"
NAMESPACE = uuid.UUID("6f1c2a52-5d3e-4c1b-9a57-1b0e8f3d2c10")
MAX_FIELD = 4 * 1024 * 1024  # TestsetDtos.MAX_CASE_FIELD_CHARS
CASES_PER_SET = 20


def js(value):
    return json.dumps(value, separators=(",", ":"), ensure_ascii=False)


# ---------------------------------------------------------------- two-sum

def two_sum(nums, target):
    seen = {}
    for j, x in enumerate(nums):
        if target - x in seen:
            return seen[target - x], j
        seen[x] = j
    raise ValueError("no pair")


def two_sum_brute(nums, target):
    pairs = [(i, j) for i in range(len(nums)) for j in range(i + 1, len(nums)) if nums[i] + nums[j] == target]
    assert len(pairs) == 1, pairs
    return pairs[0]


def pair_count(nums, target):
    seen, pairs = {}, 0
    for x in nums:
        pairs += seen.get(target - x, 0)
        seen[x] = seen.get(x, 0) + 1
    return pairs


def two_sum_case(nums, target):
    assert pair_count(nums, target) == 1, "two-sum case must have exactly one answer"
    i, j = two_sum(nums, target)
    if len(nums) <= 300:
        assert (i, j) == two_sum_brute(nums, target)
    return js({"nums": nums, "target": target}), js([i, j])


def two_sum_random(rng, n, lo, hi):
    while True:
        nums = [rng.randint(lo, hi) for _ in range(n)]
        i, j = sorted(rng.sample(range(n), 2))
        target = nums[i] + nums[j]
        if abs(target) <= 2 * 10**9 and pair_count(nums, target) == 1:
            return two_sum_case(nums, target)


def two_sum_large(rng, n, i, j):
    """n numbers where only positions i and j reach the target: the rest are multiples of 4, the pair is 1 and 2 mod 4."""
    nums = [4 * rng.randint(-249_999_999, 249_999_999) for _ in range(n)]
    nums[i] += 1
    nums[j] += 2
    return two_sum_case(nums, nums[i] + nums[j])


def two_sum_sets():
    samples = [two_sum_case([3, 8, 12, 5], 17), two_sum_case([4, -1, 4], 8)]
    edges = [
        two_sum_case([1, 2], 3),
        two_sum_case([0, 0], 0),
        two_sum_case([-5, -9, -2, -14], -11),
        two_sum_case([10**9, -(10**9), 7], 0),
        two_sum_case([6, 1, 9, 3, 5], 12),
        two_sum_case([2, 4, 6, 8, 10, 11], 21),
    ]
    edges_v2 = [
        two_sum_case([10**9, 10**9 - 1, 10**9 - 3], 2 * 10**9 - 1),
        two_sum_case([-(10**9), -(10**9) + 2, 5], -2 * 10**9 + 2),
        two_sum_case([7] + [100 + 2 * k for k in range(1, 3000)] + [13], 20),
        # Large: the pair sits where a nested-loop search finds it last.
        two_sum_large(random.Random(103), 200_000, 199_998, 199_999),
        two_sum_large(random.Random(104), 200_000, 100_000, 199_999),
    ]

    def fill(rng, fixed):
        cases = list(fixed)
        sizes = [5, 10, 50, 200, 1000, 3000, 5000]
        k = 0
        while len(cases) < CASES_PER_SET - 2:
            n = sizes[k % len(sizes)]
            spread = 10**9 if k % 2 else 1000 * n
            cases.append(two_sum_random(rng, n, -spread, spread))
            k += 1
        return samples + cases

    return fill(random.Random(101), edges), fill(random.Random(102), edges + edges_v2)


# ---------------------------------------------------------------- valid-parentheses

PAIRS = {")": "(", "]": "[", "}": "{"}


def valid_parens(s):
    stack = []
    for ch in s:
        if ch in "([{":
            stack.append(ch)
        elif not stack or stack.pop() != PAIRS[ch]:
            return False
    return not stack


def valid_parens_brute(s):
    prev = None
    while prev != s:
        prev, s = s, s.replace("()", "").replace("[]", "").replace("{}", "")
    return s == ""


def parens_case(s):
    answer = valid_parens(s)
    if len(s) <= 2000:
        assert answer == valid_parens_brute(s)
    return js({"s": s}), js(answer)


def balanced(rng, n):
    """A random balanced string of length 2n."""
    out, stack, opened = [], [], 0
    while opened < n or stack:
        if opened < n and (not stack or rng.random() < 0.55):
            ch = rng.choice("([{")
            stack.append(ch)
            out.append(ch)
            opened += 1
        else:
            out.append({"(": ")", "[": "]", "{": "}"}[stack.pop()])
    return "".join(out)


def broken(rng, n):
    """A balanced string with one change that makes it invalid."""
    s = list(balanced(rng, n))
    while True:
        t = s[:]
        kind = rng.randrange(3)
        i = rng.randrange(len(t))
        if kind == 0:
            t[i] = rng.choice([c for c in "()[]{}" if c != t[i]])
        elif kind == 1:
            del t[i]
        else:
            t.insert(i, rng.choice("()[]{}"))
        if not valid_parens("".join(t)):
            return "".join(t)


def parens_sets():
    samples = [parens_case("{[()()]}"), parens_case("([)]")]
    edges = [
        parens_case("("),
        parens_case(")"),
        parens_case("()"),
        parens_case("]["),
        parens_case("(((("),
        parens_case("()[]{}"),
        parens_case("{[}]"),
    ]
    edges_v2 = [
        parens_case("(" * 5000 + ")" * 5000),
        parens_case("(" * 5000 + ")" * 4999 + "]"),
        parens_case("()" * 4999 + "("),
        # Large and deeply nested: repeated "()" -> "" replacement needs ~10^5 passes.
        parens_case("(" * 100_000 + ")" * 100_000),
        parens_case("([{" * 33_333 + "}])" * 33_333 + ")"),
    ]

    def fill(rng, fixed):
        cases = list(fixed)
        sizes = [3, 10, 40, 200, 1000, 4999]  # length 2n, plus one from broken(): stays within 10^4
        k = 0
        while len(cases) < CASES_PER_SET - 2:
            n = sizes[k % len(sizes)]
            cases.append(parens_case(balanced(rng, n) if k % 2 == 0 else broken(rng, n)))
            k += 1
        return samples + cases

    return fill(random.Random(201), edges), fill(random.Random(202), edges + edges_v2)


# ---------------------------------------------------------------- maximum-subarray

def max_subarray(a):
    best = cur = a[0]
    for x in a[1:]:
        cur = max(x, cur + x)
        best = max(best, cur)
    return best


def max_subarray_brute(a):
    return max(sum(a[i:j + 1]) for i in range(len(a)) for j in range(i, len(a)))


def subarray_case(a):
    answer = max_subarray(a)
    if len(a) <= 300:
        assert answer == max_subarray_brute(a)
    return js({"nums": a}), js(answer)


def subarray_sets():
    samples = [subarray_case([-3, 4, -1, 2, -6, 5]), subarray_case([-8, -2, -5])]
    edges = [
        subarray_case([7]),
        subarray_case([-7]),
        subarray_case([0, 0, 0]),
        subarray_case([1, 2, 3, 4, 5]),
        subarray_case([5, -10, 5]),
        subarray_case([-1, 0, -2]),
    ]
    edges_v2 = [
        subarray_case([10**4] * 5000),
        subarray_case([-(10**4)] * 5000),
        subarray_case([10**4 if k % 2 == 0 else -(10**4) + 1 for k in range(5000)]),
        # Large: O(n²) over all runs is 2·10^10 steps.
        subarray_case([rng.randint(-10**4, 10**4) for rng in [random.Random(303)] for _ in range(200_000)]),
        subarray_case([max(-(10**4), rng.randint(-10**4, 10**4) - 3000) for rng in [random.Random(304)]
                       for _ in range(200_000)]),
    ]

    def fill(rng, fixed):
        cases = list(fixed)
        sizes = [4, 12, 60, 300, 1500, 5000]
        k = 0
        while len(cases) < CASES_PER_SET - 2:
            n = sizes[k % len(sizes)]
            bias = [0, -2000, 1500][k % 3]
            cases.append(subarray_case([max(-(10**4), min(10**4, rng.randint(-10**4, 10**4) + bias))
                                        for _ in range(n)]))
            k += 1
        return samples + cases

    return fill(random.Random(301), edges), fill(random.Random(302), edges + edges_v2)


# ---------------------------------------------------------------- longest-substring-without-repeating-characters

ALPHABET = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"


def longest_unique(s):
    last, start, best = {}, 0, 0
    for i, ch in enumerate(s):
        if last.get(ch, -1) >= start:
            start = last[ch] + 1
        last[ch] = i
        best = max(best, i - start + 1)
    return best


def longest_unique_brute(s):
    best = 0
    for i in range(len(s)):
        seen = set()
        for ch in s[i:]:
            if ch in seen:
                break
            seen.add(ch)
        best = max(best, len(seen))
    return best


def unique_case(s):
    answer = longest_unique(s)
    if len(s) <= 3000:
        assert answer == longest_unique_brute(s)
    return js({"s": s}), js(answer)


def unique_sets():
    samples = [unique_case("dvdfxd"), unique_case("zzzz")]
    edges = [
        unique_case("q"),
        unique_case("ab"),
        unique_case("aab"),
        unique_case("abba"),
        unique_case("tmmzuxt"),
        unique_case("aA1aA1"),
    ]
    edges_v2 = [
        unique_case(ALPHABET),
        unique_case(ALPHABET * 800),
        unique_case("x" * 50000),
    ]

    def fill(rng, fixed):
        cases = list(fixed)
        sizes = [8, 30, 120, 1000, 10000, 50000]
        k = 0
        while len(cases) < CASES_PER_SET - 2:
            n = sizes[k % len(sizes)]
            letters = ALPHABET[: [3, 10, 26, 62][k % 4]]
            cases.append(unique_case("".join(rng.choice(letters) for _ in range(n))))
            k += 1
        return samples + cases

    return fill(random.Random(401), edges), fill(random.Random(402), edges + edges_v2)


# ---------------------------------------------------------------- number-of-islands

def islands(grid):
    rows, cols = len(grid), len(grid[0])
    seen = [[False] * cols for _ in range(rows)]
    count = 0
    for r in range(rows):
        for c in range(cols):
            if grid[r][c] == "1" and not seen[r][c]:
                count += 1
                seen[r][c] = True
                queue = deque([(r, c)])
                while queue:
                    y, x = queue.popleft()
                    for ny, nx in ((y + 1, x), (y - 1, x), (y, x + 1), (y, x - 1)):
                        if 0 <= ny < rows and 0 <= nx < cols and grid[ny][nx] == "1" and not seen[ny][nx]:
                            seen[ny][nx] = True
                            queue.append((ny, nx))
    return count


def islands_brute(grid):
    """Union-find, a different method, for cross-checking."""
    rows, cols = len(grid), len(grid[0])
    parent = list(range(rows * cols))

    def find(i):
        while parent[i] != i:
            parent[i] = parent[parent[i]]
            i = parent[i]
        return i

    for r in range(rows):
        for c in range(cols):
            if grid[r][c] == "1":
                for nr, nc in ((r + 1, c), (r, c + 1)):
                    if nr < rows and nc < cols and grid[nr][nc] == "1":
                        parent[find(r * cols + c)] = find(nr * cols + nc)
    return len({find(r * cols + c) for r in range(rows) for c in range(cols) if grid[r][c] == "1"})


def islands_case(grid):
    answer = islands(grid)
    assert answer == islands_brute(grid)
    return js({"grid": grid}), js(answer)


def spiral(n):
    """One snake-shaped island that winds through an n x n grid (deep for recursive DFS)."""
    g = [["0"] * n for _ in range(n)]
    for r in range(0, n, 2):
        for c in range(n):
            g[r][c] = "1"
        if r + 1 < n:
            g[r + 1][n - 1 if (r // 2) % 2 == 0 else 0] = "1"
    return ["".join(row) for row in g]


def islands_sets():
    samples = [
        islands_case(["11000", "11010", "00011", "01000"]),
        islands_case(["101", "010", "101"]),
    ]
    edges = [
        islands_case(["0"]),
        islands_case(["1"]),
        islands_case(["0000", "0000"]),
        islands_case(["111", "111", "111"]),
        islands_case(["1010101"]),
        islands_case(["1", "1", "0", "1"]),
        islands_case(["111", "101", "111"]),
    ]
    edges_v2 = [
        islands_case(["".join("1" if (r + c) % 2 == 0 else "0" for c in range(200)) for r in range(200)]),
        islands_case(spiral(199)),
        islands_case(["1" * 200] * 200),
        # Large: a million cells, and a snake island 5·10^5 cells deep for recursive DFS.
        islands_case(["".join("1" if rng.random() < 0.45 else "0" for _ in range(1000))
                      for rng in [random.Random(503)] for _ in range(1000)]),
        islands_case(spiral(1000)),
    ]

    def fill(rng, fixed):
        cases = list(fixed)
        shapes = [(3, 4), (10, 10), (25, 40), (60, 60), (120, 150), (200, 200)]
        k = 0
        while len(cases) < CASES_PER_SET - 2:
            rows, cols = shapes[k % len(shapes)]
            land = [0.3, 0.45, 0.6][k % 3]
            cases.append(islands_case(["".join("1" if rng.random() < land else "0" for _ in range(cols))
                                       for _ in range(rows)]))
            k += 1
        return samples + cases

    return fill(random.Random(501), edges), fill(random.Random(502), edges + edges_v2)


# ---------------------------------------------------------------- problems

def problem(slug, title, difficulty, points, sets, function_name, parameters, return_type, python_signature, statement,
            time_limit_ms=1000):
    starter = f"class Solution:\n    def {function_name}(self, {python_signature}:\n        pass\n"
    return dict(slug=slug, title=title, difficulty=difficulty, points=points, sets=sets, function_name=function_name,
                parameters=[{"name": n, "type": t} for n, t in parameters], return_type=return_type,
                starter_code=starter, statement=statement, time_limit_ms=time_limit_ms)


PROBLEMS = [
    problem("two-sum", "Two Sum", "easy", 100, two_sum_sets, "twoSum", [("nums", "int[]"), ("target", "int")], "int[]",
            "nums: list[int], target: int) -> list[int]", """\
Given a list of integers and a target, find the two positions whose values add up to the target.

Exactly one pair of positions works. A position cannot be used twice. Return the two 0-based positions, smallest first.

### Constraints
- 2 ≤ length of `nums` ≤ 2 × 10⁵
- −10⁹ ≤ each integer ≤ 10⁹
- Exactly one answer exists.

### Example
`nums = [3, 8, 12, 5]`, `target = 17` returns `[2, 3]`: 12 + 5 = 17, at positions 2 and 3.
"""),
    problem("valid-parentheses", "Valid Parentheses", "easy", 100, parens_sets, "isValid", [("s", "string")], "bool",
            "s: str) -> bool", """\
A string contains only the characters `( ) [ ] { }`. Return whether it is balanced:

- every opening bracket is closed by a bracket of the same type, and
- brackets close in the reverse order they were opened.

### Constraints
- 1 ≤ length of `s` ≤ 2 × 10⁵

### Example
`s = "{[()()]}"` returns `true`.
"""),
    problem("maximum-subarray", "Maximum Subarray", "medium", 200, subarray_sets, "maxSubArray", [("nums", "int[]")], "int",
            "nums: list[int]) -> int", """\
Given a list of integers, return the largest sum of a contiguous, non-empty run of them.

### Constraints
- 1 ≤ length of `nums` ≤ 2 × 10⁵
- −10⁴ ≤ each integer ≤ 10⁴

### Example
`nums = [-3, 4, -1, 2, -6, 5]` returns `5`: the run `4, -1, 2` sums to 5, and so does `5` on its own.
"""),
    problem("longest-substring-without-repeating-characters", "Longest Substring Without Repeating Characters", "medium",
            200, unique_sets, "lengthOfLongestSubstring", [("s", "string")], "int", "s: str) -> int", """\
Given a string, return the length of the longest contiguous piece of it in which no character appears twice.

`s` contains only letters (`a-z`, `A-Z`) and digits; upper and lower case are different characters.

### Constraints
- 1 ≤ length of `s` ≤ 5 × 10⁴

### Example
`s = "dvdfxd"` returns `4`: the longest piece is `vdfx`.
"""),
    problem("number-of-islands", "Number of Islands", "hard", 300, islands_sets, "numIslands", [("grid", "string[]")], "int",
            "grid: list[str]) -> int", """\
A map is a list of equal-length strings, one per row, made of `1` (land) and `0` (water). Land cells that touch up,
down, left or right belong to the same island; touching only at a corner does not connect them. Return the number of
islands.

### Constraints
- 1 ≤ rows, columns ≤ 1000

### Example
`grid = ["11000", "11010", "00011", "01000"]` returns `3`.
""", time_limit_ms=2000),
]


# ---------------------------------------------------------------- SQL output

def content_hash(cases):
    """Same as TestsetService.contentHash: SHA-256 over length-prefixed input, output and a sample byte, per case."""
    md = hashlib.sha256()
    for inp, out, sample in cases:
        for field in (inp, out):
            data = field.encode("utf-8")
            md.update(struct.pack(">i", len(data)))
            md.update(data)
        md.update(b"\x01" if sample else b"\x00")
    return md.hexdigest()


def lit(s):
    return "E'" + s.replace("\\", "\\\\").replace("'", "\\'").replace("\n", "\\n") + "'"


def problem_sql(p):
    slug = p["slug"]
    pid = uuid.uuid5(NAMESPACE, f"problem/{slug}")
    versions = []
    for v, cases in enumerate(p["sets"](), start=1):
        assert len(cases) == CASES_PER_SET, (slug, v, len(cases))
        rows = [(inp, out, i < 2) for i, (inp, out) in enumerate(cases)]
        for inp, out, _ in rows:
            assert len(inp) <= MAX_FIELD and len(out) <= MAX_FIELD, slug
        versions.append(rows)
    assert versions[0][:2] == versions[1][:2], "samples stay the same across versions"
    assert versions[0] != versions[1]

    lines = [
        f"-- {p['title']}",
        "DO $$",
        "BEGIN",
        f"    IF EXISTS (SELECT 1 FROM problems WHERE id = '{pid}' OR slug = '{slug}') THEN",
        f"        RAISE NOTICE 'sample problem {slug} already exists, skipped';",
        "        RETURN;",
        "    END IF;",
        "",
        "    INSERT INTO problems (id, slug, title, statement_md, difficulty, points, time_limit_ms, memory_limit_mb,",
        "                          function_name, parameters, return_type, starter_code)",
        f"    VALUES ('{pid}', '{slug}', {lit(p['title'])}, {lit(p['statement'])}, '{p['difficulty']}', "
        f"{p['points']}, {p['time_limit_ms']}, 256,",
        f"            {lit(p['function_name'])}, {lit(js(p['parameters']))}::jsonb, {lit(p['return_type'])}, "
        f"{lit(p['starter_code'])});",
        "",
        "    INSERT INTO testsets (problem_id, version, content_hash) VALUES",
        ",\n".join(f"        ('{pid}', {v}, '{content_hash(rows)}')" for v, rows in enumerate(versions, start=1)) + ";",
    ]
    for v, rows in enumerate(versions, start=1):
        values = []
        for ordinal, (inp, out, sample) in enumerate(rows, start=1):
            cid = uuid.uuid5(NAMESPACE, f"case/{slug}/{v}/{ordinal}")
            values.append(f"        ('{cid}', '{pid}', {v}, {ordinal}, {lit(inp)}, {lit(out)}, "
                          f"{'TRUE' if sample else 'FALSE'})")
        lines += [
            "",
            f"    -- version {v}",
            "    INSERT INTO test_cases (id, problem_id, testset_version, ordinal, input, expected_output, is_sample) VALUES",
            ",\n".join(values) + ";",
        ]
    lines += [
        "",
        "    -- Publish after the cases are in: published sets are immutable (see V2).",
        f"    UPDATE testsets SET status = 'published', published_at = now() WHERE problem_id = '{pid}';",
        f"    UPDATE problems SET status = 'published', current_version = {len(versions)}, updated_at = now()"
        f" WHERE id = '{pid}';",
        "END",
        "$$;",
    ]
    return "\n".join(lines)


def main():
    header = """\
-- GENERATED by scripts/generate_sample_data.py. Do not edit by hand: change the script and run it again.
--
-- Five sample problems for local testing, each with published test-set versions 1 and 2 (2 is current),
-- 20 cases per version, the first two of which are samples. Loaded only by the local profile
-- (spring.flyway.locations in application-local.properties), never on OpenShift.
--
-- Repeatable migration: Flyway re-runs it whenever this file changes. A problem whose id or slug already
-- exists is skipped, so a re-run never touches existing (possibly immutable) data.
"""
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(header + "\n" + "\n\n".join(problem_sql(p) for p in PROBLEMS) + "\n", encoding="utf-8")
    for p in PROBLEMS:
        print(f"{uuid.uuid5(NAMESPACE, 'problem/' + p['slug'])}  {p['slug']}")
    print(f"wrote {OUT} ({OUT.stat().st_size // 1024} KB)")


if __name__ == "__main__":
    main()
