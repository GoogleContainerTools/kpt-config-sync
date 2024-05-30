package bytes

type Bit int

type Byte int

const (
	ByteSize Bit = 8

	Kb Bit = 1000      // Kilobit
	Mb Bit = 1000 * Kb // Megabit
	Gb Bit = 1000 * Mb // Gigabit
	Tb Bit = 1000 * Gb // Terabit
	Pb Bit = 1000 * Tb // Petabit
	Eb Bit = 1000 * Pb // Exabit

	KB Byte = 1000      // Kilobyte
	MB Byte = 1000 * KB // Megabyte
	GB Byte = 1000 * MB // Gigabyte
	TB Byte = 1000 * GB // Terabyte
	PB Byte = 1000 * TB // Petabyte
	EB Byte = 1000 * PB // Exabyte

	KiB Byte = 1024       // Kibibyte
	MiB Byte = 1024 * KiB // Mebibyte
	GiB Byte = 1024 * MiB // Gibibyte
	TiB Byte = 1024 * GiB // Tebibyte
	PiB Byte = 1024 * TiB // Pebibyte
	EiB Byte = 1024 * PiB // Exbibyte
)
