package generator

type Generator interface {
	Generate(count int) error
}
