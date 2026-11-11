// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import NewProjectDialog from '../src/components/NewProjectDialog.svelte';
import { isLocalHost, passwordVar } from '../src/lib/server';
import type { DbTestResult, ProjectInfo } from '../src/lib/types';

const found = (over: Partial<DbTestResult> = {}): DbTestResult => ({
  reachable: true,
  non_local: false,
  database_exists: true,
  empty: true,
  has_board: false,
  password_var: 'WAYPOINT_DB_PASSWORD_KITCHEN',
  global_password_var: 'WAYPOINT_DB_PASSWORD',
  ...over,
});

const made = (name: string): ProjectInfo => ({ name, storage: 'postgres', host: 'localhost', port: 5432, database: 'kitchen', user: 'waypoint', accent: 'orange' });

function open(testDatabase = vi.fn(async () => found())) {
  const createProject = vi.fn(async (p: { name: string }) => made(p.name));
  const onCreated = vi.fn();
  render(NewProjectDialog, { props: { client: { createProject, testDatabase }, initialTab: 'create', onClose: vi.fn(), onCreated } });
  return { createProject, testDatabase, onCreated };
}

const field = (label: string) => screen.getByLabelText(label) as HTMLInputElement;
const typeInto = (label: string, value: string) => fireEvent.input(field(label), { target: { value } });
const createButton = () => screen.getByRole('button', { name: 'Create project' });

async function chooseServer(name = 'Kitchen') {
  await typeInto('Name', name);
  await fireEvent.click(screen.getByLabelText('Database server (Postgres)'));
}

describe('helpers', () => {
  it('names the password variable like the server does', () => {
    expect(passwordVar('pharmacy')).toBe('WAYPOINT_DB_PASSWORD_PHARMACY');
    expect(passwordVar('my-proj 2')).toBe('WAYPOINT_DB_PASSWORD_MY_PROJ_2');
  });

  it('knows which hosts are on this computer', () => {
    for (const h of ['localhost', 'LOCALHOST', '127.0.0.1', '127.5.5.5', '::1', '[::1]', '/var/run/postgresql']) expect(isLocalHost(h)).toBe(true);
    for (const h of ['db.example.com', '192.168.1.5', '10.0.0.2', 'localhost.example.com']) expect(isLocalHost(h)).toBe(false);
  });
});

describe('creating a project on a database server', () => {
  it('shows the exact password variables and has no password field', async () => {
    open();
    await chooseServer('Kitchen');
    expect(screen.getByText('WAYPOINT_DB_PASSWORD_KITCHEN')).toBeInTheDocument();
    expect(screen.getByText('WAYPOINT_DB_PASSWORD')).toBeInTheDocument();
    expect(document.querySelector('input[type="password"]')).toBeNull();
    expect(screen.queryByLabelText(/password/i)).toBeNull();
  });

  it('warns only about a server that is not on this computer', async () => {
    open();
    await chooseServer();
    expect(screen.queryByText(/not on this computer/i)).toBeNull();
    await typeInto('Host', 'db.example.com');
    expect(screen.getByText(/not on this computer/i)).toBeInTheDocument();
    expect(screen.getByLabelText('I understand')).not.toBeChecked();
    await typeInto('Host', '127.0.0.1');
    expect(screen.queryByText(/not on this computer/i)).toBeNull();
  });

  it('keeps Create disabled until Test connection passes, and again after an edit', async () => {
    const { testDatabase } = open();
    await chooseServer();
    await typeInto('User', 'waypoint');
    expect(createButton()).toBeDisabled();
    await fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    await waitFor(() => expect(createButton()).toBeEnabled());
    expect(testDatabase).toHaveBeenCalledWith({ engine: 'postgres', project: 'Kitchen', host: 'localhost', port: 5432, user: 'waypoint', database: '', ssl: '' });
    await typeInto('Database', 'other');
    expect(createButton()).toBeDisabled();
  });

  it('also wants the warning acknowledged for a server that is not local', async () => {
    open(vi.fn(async () => found({ non_local: true })));
    await chooseServer();
    await typeInto('Host', 'db.example.com');
    await typeInto('User', 'waypoint');
    await fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    await screen.findByText(/Connected/);
    expect(createButton()).toBeDisabled();
    await fireEvent.click(screen.getByLabelText('I understand'));
    expect(createButton()).toBeEnabled();
  });

  it('explains a server that cannot be reached and stays disabled', async () => {
    open(vi.fn(async () => found({ reachable: false, error: 'cannot reach the database server at localhost:5432' })));
    await chooseServer();
    await typeInto('User', 'waypoint');
    await fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('cannot reach the database server');
    expect(createButton()).toBeDisabled();
  });

  it('refuses a database that holds something that is not a board', async () => {
    open(vi.fn(async () => found({ empty: false, has_board: false })));
    await chooseServer();
    await typeInto('User', 'waypoint');
    await fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    expect(await screen.findByText(/holds other tables/i)).toBeInTheDocument();
    expect(createButton()).toBeDisabled();
  });

  it('needs the box ticked to make a database that is not there yet', async () => {
    open(vi.fn(async () => found({ database_exists: false, empty: false })));
    await chooseServer();
    await typeInto('User', 'waypoint');
    await fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/database does not exist/i);
    expect(createButton()).toBeDisabled();
    await fireEvent.click(screen.getByLabelText('Create the database if it does not exist'));
    expect(createButton()).toBeEnabled();
  });

  it('sends the server settings and never a password', async () => {
    const { createProject, onCreated } = open();
    await chooseServer();
    await typeInto('User', 'waypoint');
    await typeInto('Database', 'kitchen');
    await fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    await waitFor(() => expect(createButton()).toBeEnabled());
    await fireEvent.click(createButton());
    await waitFor(() => expect(createProject).toHaveBeenCalled());
    expect(createProject).toHaveBeenCalledWith({
      name: 'Kitchen',
      prefix: 'T',
      storage: 'postgres',
      host: 'localhost',
      port: 5432,
      user: 'waypoint',
      database: 'kitchen',
    });
    expect(JSON.stringify(createProject.mock.calls[0])).not.toMatch(/password/i);
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('Kitchen'));
  });
});

describe('a passing test only counts for what was tested', () => {
  async function tested(extra: Partial<DbTestResult> = {}) {
    open(vi.fn(async () => found(extra)));
    await chooseServer();
    await typeInto('User', 'waypoint');
    await fireEvent.click(screen.getByRole('button', { name: 'Test connection' }));
    await waitFor(() => expect(createButton()).toBeEnabled());
  }

  it('is forgotten when the project is renamed, and the variable name follows', async () => {
    await tested();
    expect(screen.getByText('WAYPOINT_DB_PASSWORD_KITCHEN')).toBeInTheDocument();
    await typeInto('Name', 'Pantry');
    expect(createButton()).toBeDisabled();
    expect(screen.getByText('WAYPOINT_DB_PASSWORD_PANTRY')).toBeInTheDocument();
    expect(screen.queryByText('WAYPOINT_DB_PASSWORD_KITCHEN')).toBeNull();
  });

  it('un-ticks "I understand" when the host changes', async () => {
    open(vi.fn(async () => found({ non_local: true })));
    await chooseServer();
    await typeInto('Host', 'db.one.example');
    await typeInto('User', 'waypoint');
    await fireEvent.click(screen.getByLabelText('I understand'));
    await typeInto('Host', 'db.two.example');
    expect(screen.getByLabelText('I understand')).not.toBeChecked();
  });
});
