package engine

func (e *Engine) getOrCreateReviewSession(runID, producerID string, kind sessionKind) *reviewSession {
	return e.reviewSess.GetOrCreate(e.laneSessionKey(runID, producerID, ""), func() *reviewSession {
		return e.newReviewSession(runID, producerID, "", "", kind)
	})
}
