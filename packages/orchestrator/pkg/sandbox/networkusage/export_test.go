package networkusage

// These constructors exist only in the networkusage test binary. They do not
// authorize a producer/reader or bypass the production credential constructors.
func OpenProtectedServiceForTest(dir string, options SpoolOptions, config ProtectedDeliveryConfig, store protectedStore) (*Service, error) {
	if config.Custody.Destination.MaxObjectBytes < options.SegmentBytes {
		return nil, ErrSpoolBudget
	}
	return openProtectedService(dir, options, config, store)
}

func OpenEvidenceConsumerForTest(dir string, options ConsumerOptions, reader evidenceVersionReader) (*EvidenceConsumer, error) {
	identity, err := ConsumerBuildIdentity()
	if err != nil {
		return nil, err
	}
	return newEvidenceConsumer(dir, options, reader, identity)
}
