package llm

// streamRequestForTest fills the message list of a transport request.
func streamRequestForTest(request StreamRequest, messages []Message) StreamRequest {
	request.Messages = messages
	return request
}
