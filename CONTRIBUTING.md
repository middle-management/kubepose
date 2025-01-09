# Contributing to kubepose

Thank you for your interest in contributing to kubepose! We aim to keep this project focused and simple, while maintaining high quality standards.

## Development Setup

1. Install Go 1.21 or later
2. Clone the repository:
   ```bash
   git clone https://github.com/slaskis/kubepose.git
   cd kubepose
   ```

## Development Workflow

1. Create a new branch for your changes:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes and ensure tests pass:
   ```bash
   go test ./...
   ```

3. Format your code:
   ```bash
   go fmt ./...
   ```

## Pull Request Guidelines

1. **Keep It Simple**
   - Focus on one feature or fix per PR
   - Maintain the project's minimalist philosophy

2. **Documentation**
   - Update README.md if adding new features
   - Include clear commit messages
   - Add comments for complex logic

3. **Testing**
   - Add tests for new features
   - Ensure all tests pass
   - Include relevant test cases

4. **Code Style**
   - Follow Go best practices
   - Use consistent formatting
   - Keep functions focused and small

## Issue Guidelines

1. **Bug Reports**
   - Include kubepose version
   - Provide minimal compose file example
   - Describe expected vs actual behavior

2. **Feature Requests**
   - Explain the use case
   - Provide example compose configuration
   - Consider alignment with project goals

## Release Process

1. Maintainers will handle versioning
2. Releases follow semantic versioning
3. Release notes should be clear and concise

## Code of Conduct

- Be respectful and inclusive
- Focus on constructive feedback
- Help maintain a positive environment

## Questions?

Feel free to:
- Open an issue for discussion
- Ask questions in PR comments
- Reach out to maintainers

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
