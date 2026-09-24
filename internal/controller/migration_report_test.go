package controller

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestParseMigrationReport_Empty(t *testing.T) {
	g := gomega.NewWithT(t)
	report, err := parseMigrationReport("")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(report).To(gomega.Equal(migrationReport{}))
}

func TestParseMigrationReport_WellFormed(t *testing.T) {
	g := gomega.NewWithT(t)
	report, err := parseMigrationReport(`{"streams":[{"name":"runtime","appliedVersion":"5","appliedCount":2}]}`)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(report.Streams).To(gomega.HaveLen(1))
	g.Expect(report.Streams[0].Name).To(gomega.Equal("runtime"))
	g.Expect(report.Streams[0].AppliedVersion).To(gomega.Equal("5"))
	g.Expect(report.Streams[0].AppliedCount).To(gomega.Equal(int32(2)))
	g.Expect(report.Error).To(gomega.BeEmpty())
}

func TestParseMigrationReport_WithError(t *testing.T) {
	g := gomega.NewWithT(t)
	report, err := parseMigrationReport(`{"streams":[{"name":"runtime"}],"error":"checksum mismatch on V3"}`)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(report.Error).To(gomega.Equal("checksum mismatch on V3"))
}

func TestParseMigrationReport_Malformed(t *testing.T) {
	g := gomega.NewWithT(t)
	_, err := parseMigrationReport("{not json")
	g.Expect(err).To(gomega.HaveOccurred())
}
