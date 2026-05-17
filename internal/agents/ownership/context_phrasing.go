package ownership

func phraseContext(c candidate) string {
	switch c.Source {
	case "blame":
		return "has recent changes in " + c.Reason
	case "cochange":
		return "co-changes with " + c.Reason
	default:
		return "has touched " + c.Reason
	}
}
