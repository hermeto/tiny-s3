# Quickstart: Ordered Index and ListObjectsV2

Contract: [contracts/http-list-objects-v2.md](contracts/http-list-objects-v2.md).

## Quality gates

```sh
make vet test race cover e2e
```

Expected: all exit 0, 100% for every `internal/` package, `coverage gate: PASSED`; `make e2e` includes listing with the CLI (order, fields, prefix, delimiter, `aws s3 ls`, special characters, start-after, restart), and paging over 2,500 keys while another client writes.

## Compose and AWS CLI check

```sh
docker compose up -d --build
export AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 AWS_DEFAULT_OUTPUT=json AWS_PAGER=""
s3() { aws --endpoint-url http://localhost:9000 s3api "$@"; }
printf 'hello world' > /tmp/tiny-s3-hello.txt
s3 create-bucket --bucket demo >/dev/null
for k in top dir/f1 dir/sub/f2 dir/sub/f3 'dir/a b+c.txt'; do
  s3 put-object --bucket demo --key "$k" --body /tmp/tiny-s3-hello.txt >/dev/null
done
s3 list-objects-v2 --bucket demo --query 'Contents[].[Key,Size,StorageClass]' --output text   # 1
s3 list-objects-v2 --bucket demo --prefix dir/ --delimiter / --no-paginate \
  --query '{k:Contents[].Key,p:CommonPrefixes[].Prefix,n:KeyCount}'                           # 2
aws --endpoint-url http://localhost:9000 s3 ls s3://demo/dir/                                # 3
s3 list-objects-v2 --bucket demo --max-keys 2 --no-paginate --query '[KeyCount,IsTruncated,NextContinuationToken!=null]'   # 4
s3 list-objects-v2 --bucket demo --start-after dir/sub/f2 --query 'Contents[].Key'           # 5
s3 delete-object --bucket demo --key top
s3 list-objects-v2 --bucket demo --query 'Contents[].Key'                                    # 6
s3 list-objects-v2 --bucket missing                                                          # 7
rm /tmp/tiny-s3-hello.txt
docker compose down -v
```

`--no-paginate` shows one server response; without it the CLI follows the tokens itself and drops per-page fields such as `KeyCount`.

Expected:

1. Five lines in byte order: `dir/a b+c.txt`, `dir/f1`, `dir/sub/f2`, `dir/sub/f3`, `top`, each with `11` and `STANDARD`.
2. `k` = `["dir/a b+c.txt", "dir/f1"]`, `p` = `["dir/sub/"]`, `n` = 3.
3. `PRE sub/` and the two files under `dir/`.
4. `[2, true, true]`.
5. `["dir/sub/f3", "top"]`.
6. Four keys; `top` is gone.
7. Exit 254, `(NoSuchBucket)`.
