const { Kysely, PostgresDialect } = require('kysely');
const { Pool } = require('pg');

function getDatabaseUrl() {
  return process.env.DATABASE_URL || 'postgres://postgres:postgres@host.docker.internal:5432/postgres';
}

exports.handler = async function handler() {
  const databaseUrl = new URL(getDatabaseUrl());
  const host = databaseUrl.hostname;
  const port = Number(databaseUrl.port || '5432');
  const database = databaseUrl.pathname.replace(/^\//, '') || 'postgres';

  const pool = new Pool({
    connectionString: databaseUrl.toString()
  });

  const db = new Kysely({
    dialect: new PostgresDialect({ pool })
  });

  let client;
  try {
    client = await pool.connect();
    client.release();
    client = null;

    const result = {
      function: process.env.AWS_LAMBDA_FUNCTION_NAME || 'unknown',
      library: 'kysely',
      dialect: 'postgres',
      host,
      port,
      database,
      connected: true
    };

    console.log(JSON.stringify(result));
    return result;
  } finally {
    if (client) {
      client.release();
    }
    await db.destroy();
  }
};
