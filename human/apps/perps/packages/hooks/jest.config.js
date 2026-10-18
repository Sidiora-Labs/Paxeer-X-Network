module.exports = {
    "roots": [
      "<rootDir>"
    ],
    "testMatch": [
      "**/__tests__/**/*.+(ts|tsx|js)",
      "**/?(*.)+(spec|test).+(ts|tsx|js)"
    ],
    testEnvironment: 'jsdom',
    // transform: {
    //     '^.+\\.tsx?$': 'babel-jest',
    // },
  }
