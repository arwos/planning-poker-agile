import eslint from '@eslint/js';
import tseslint from 'typescript-eslint';
import angular from '@angular-eslint/eslint-plugin';

export default tseslint.config(
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  { files: ['src/**/*.ts'], plugins: { '@angular-eslint': angular }, rules: { '@angular-eslint/component-class-suffix': 'error', '@angular-eslint/component-selector': ['error', { type: 'element', prefix: 'app', style: 'kebab-case' }], '@typescript-eslint/explicit-function-return-type': 'error' } }
);
