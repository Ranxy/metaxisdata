package llm

// FailureCode is the stable machine-readable classifier of one failed LLM call.
// The values are part of the diagnostics contract (the API exposes them verbatim),
// so they must not change once shipped.
type FailureCode string

const (
	// FailureTruncated reports a provider stop at the output token limit: the
	// response is incomplete (finish_reason=length).
	FailureTruncated FailureCode = "TRUNCATED"
	// FailureStreamIncomplete reports a stream that ended without a finish reason
	// or the explicit [DONE] marker.
	FailureStreamIncomplete FailureCode = "STREAM_INCOMPLETE"
	// FailureProviderStatus reports a non-OK HTTP status from the provider.
	FailureProviderStatus FailureCode = "PROVIDER_STATUS"
	// FailureProviderRequest reports a request that could not be made or read.
	FailureProviderRequest FailureCode = "PROVIDER_REQUEST"
	// FailureResponseTooLarge reports a response over the size cap.
	FailureResponseTooLarge FailureCode = "RESPONSE_TOO_LARGE"
	// FailureUnexpectedFinish reports a finish reason outside the accepted set.
	FailureUnexpectedFinish FailureCode = "UNEXPECTED_FINISH_REASON"
	// FailureTimeout reports a stalled stream.
	FailureTimeout FailureCode = "TIMEOUT"
	// FailureMalformed reports a response body that is not the contracted shape.
	FailureMalformed FailureCode = "MALFORMED_RESPONSE"
	// FailureUnknown reports a call that failed for a reason outside the typed
	// vocabulary above.
	FailureUnknown FailureCode = "UNKNOWN"
)

// ResponseError is one failed provider interaction with a stable classifier. The
// underlying error keeps the human message the callers and logs already show; the
// classifier, finish reason and status are the machine-readable context.
type ResponseError struct {
	Code FailureCode
	// FinishReason is the provider's stop reason when it reported one.
	FinishReason string
	// StatusCode is the provider's HTTP status for a non-OK response.
	StatusCode int
	// OutputTokens is the output budget the failing request actually carried, when
	// a request was sent. It differs from the configured budget when the one-shot
	// smaller-budget retry was used, and stays 0 for transport/request failures
	// where nothing reached the provider.
	OutputTokens int
	Err          error
}

// Error returns the underlying message, so existing callers and log lines keep
// their wording. A ResponseError without a cause names its classifier instead.
func (e *ResponseError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

// Unwrap exposes the cause to errors.Is/errors.As.
func (e *ResponseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
