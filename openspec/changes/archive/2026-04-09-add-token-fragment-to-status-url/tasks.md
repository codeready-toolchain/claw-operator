## 1. Update Status Logic

- [x] 1.1 Add method to fetch gateway token from `openclaw-secrets` Secret with Base64 decoding
- [x] 1.2 Modify `updateStatus()` method to retrieve gateway token during status update
- [x] 1.3 Append token as URL fragment (`#token=<value>`) when constructing status URL
- [x] 1.4 Handle Secret read failures gracefully (continue with URL without token fragment)

## 2. Testing

- [x] 2.1 Add unit test for token retrieval and Base64 decoding from Secret
- [x] 2.2 Add unit test for URL construction with token fragment
- [x] 2.3 Add unit test for URL construction when Secret read fails (no fragment)
- [x] 2.4 Add integration test verifying status.url includes token fragment after reconciliation
- [x] 2.5 Verify existing status URL tests still pass with updated format

## 3. Validation

- [x] 3.1 Run `make test` to verify all unit tests pass
- [x] 3.2 Run `make lint` to verify code quality
- [ ] 3.3 Manually test against a cluster to verify status URL includes token and is accessible
