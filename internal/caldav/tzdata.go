package caldav

// Events name their times by IANA zone, and the deployment image is distroless
// with no zone database on disk, so carry one in the binary.
import _ "time/tzdata"
